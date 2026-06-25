package filemaker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultIdleTimeout is the idle duration after which WithReauthOnIdle refreshes
// the token, set just under FileMaker's default 15-minute session timeout.
const DefaultIdleTimeout = 14 * time.Minute

// Client is a handle to a FileMaker Data API session. It is safe for concurrent
// use by multiple goroutines: the session token and last-activity timestamp are
// guarded by an internal RWMutex, and the underlying *http.Client is itself
// concurrency-safe.
//
// Records returned by the client are plain data carriers; all operations that
// touch the host are methods on the Client.
type Client struct {
	httpClient           *http.Client
	host                 string
	database             string
	username             string
	password             string
	reauthOnInvalidToken bool
	idleTimeout          time.Duration // > 0 enables proactive (idle) reauth
	location             *time.Location

	reauthSem    chan struct{} // cap-1 channel used as a context-aware mutex serializing re-auth
	mu           sync.RWMutex
	token        string
	lastActivity time.Time
}

// Option configures a Client created with New.
type Option func(*config)

// config holds the optional parameters applied by Option values in New.
type config struct {
	timeout              time.Duration
	reauthOnInvalidToken bool
	idleTimeout          time.Duration
	location             *time.Location
	allowInsecureHTTP    bool
}

// WithTimeout sets the timeout applied to every HTTP request made by the
// client. The default is 30 seconds; a timeout of 0 disables it entirely.
func WithTimeout(timeout time.Duration) Option {
	return func(c *config) {
		c.timeout = timeout
	}
}

// WithReauthOnInvalidToken enables reactive re-authentication: when a request
// fails because the session token is invalid or has expired (FileMaker error
// 952), the client re-authenticates once and retries the request. Disabled by
// default, in which case the error surfaces to the caller.
//
// Combine with WithReauthOnIdle for full coverage: proactive refresh avoids most
// invalid-token errors, and this reactive retry backstops any that still occur.
func WithReauthOnInvalidToken() Option {
	return func(c *config) {
		c.reauthOnInvalidToken = true
	}
}

// WithReauthOnIdle enables proactive re-authentication: before a request, if the
// session has been idle at least timeout, the client refreshes the token first,
// so a burst of requests after an idle period does not each fail with an
// invalid-token error. With no argument it uses DefaultIdleTimeout.
//
// Combine with WithReauthOnInvalidToken so that any expiry the idle heuristic
// misses (e.g. a server timeout shorter than timeout) is still recovered.
func WithReauthOnIdle(timeout ...time.Duration) Option {
	return func(c *config) {
		d := DefaultIdleTimeout
		if len(timeout) > 0 && timeout[0] > 0 {
			d = timeout[0]
		}
		c.idleTimeout = d
	}
}

// WithInsecureHTTP permits the client to connect to a host over plaintext
// http://. This sends the credentials and session token unencrypted and must
// only be used for local development or testing against a trusted host. Without
// it, New rejects any host whose scheme is not https.
func WithInsecureHTTP() Option {
	return func(c *config) {
		c.allowInsecureHTTP = true
	}
}

// WithLocation sets the time zone used to interpret FileMaker date and timestamp
// fields (which carry no zone) when reading them back through a record's
// Time/TimeE methods or Decode. Records returned by the client carry this
// location. Defaults to UTC.
func WithLocation(loc *time.Location) Option {
	return func(c *config) {
		c.location = loc
	}
}

// New builds a client for the given host and credentials. It performs no
// network I/O: the session is established lazily, on the first operation that
// needs it (or eagerly via Authenticate). The errors it returns are therefore
// cheap argument/host validation, never a failed login.
//
// The host may include a scheme; if it does not, https is assumed. Plaintext
// http is rejected unless WithInsecureHTTP is passed, and any scheme other than
// http or https is rejected outright.
func New(host, database, username, password string, opts ...Option) (*Client, error) {
	switch {
	case host == "":
		return nil, errors.New("filemaker: no host specified")
	case database == "":
		return nil, errors.New("filemaker: no database specified")
	case username == "":
		return nil, errors.New("filemaker: no username specified")
	}

	cfg := config{timeout: 30 * time.Second, location: time.UTC}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	normalizedHost, err := normalizeHost(host, cfg.allowInsecureHTTP)
	if err != nil {
		return nil, err
	}

	jar, _ := cookiejar.New(nil)
	return &Client{
		httpClient:           &http.Client{Timeout: cfg.timeout, Jar: jar},
		host:                 normalizedHost,
		database:             database,
		username:             username,
		password:             password,
		reauthOnInvalidToken: cfg.reauthOnInvalidToken,
		idleTimeout:          cfg.idleTimeout,
		location:             cfg.location,
		reauthSem:            make(chan struct{}, 1),
	}, nil
}

// Authenticate eagerly establishes a session by logging in to the host, so a
// caller can surface credential or connectivity errors at a chosen point rather
// than on the first operation. It is optional: every operation authenticates
// lazily on first use when no session exists yet.
//
// Calling it always performs a fresh login and replaces the stored token. It
// does not log out an existing session first: the previous token is abandoned,
// not invalidated, and lingers on the host until it times out. Call Logout
// before Authenticate if you need the old session torn down promptly.
// Concurrent calls collapse to a single login, so a burst of callers does not
// produce a burst of sessions.
func (c *Client) Authenticate(ctx context.Context) error {
	c.mu.RLock()
	used := c.token
	c.mu.RUnlock()
	return c.authenticate(ctx, used)
}

// ensureAuthenticated acquires the initial session token on first use (lazy
// authentication). It is a cheap read-and-return once a session exists;
// concurrent first-use callers collapse to a single login via authenticate's
// observed-token de-duplication (an empty token is the de-dup key).
func (c *Client) ensureAuthenticated(ctx context.Context) error {
	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()
	if token != "" {
		return nil
	}
	return c.authenticate(ctx, "")
}

// Logout ends the current database session, invalidating the token on the
// host. The client remains usable afterward: a subsequent operation (or
// Authenticate) establishes a fresh session. Logout is a no-op when no session
// has been established yet.
func (c *Client) Logout(ctx context.Context) error {
	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()
	if token == "" {
		return nil
	}

	// Logout identifies the session by the token in the URL and takes no
	// Authorization header, so it bypasses the authed do() path.
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL()+"/sessions/"+token, nil)
	if err != nil {
		return fmt.Errorf("filemaker: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	var rb responseBody
	if err := c.send(req, &rb); err != nil {
		return err
	}

	c.mu.Lock()
	c.token = ""
	c.lastActivity = time.Now()
	c.mu.Unlock()
	return nil
}

// LastActivity returns the time of the last successful request made with the
// client. It is the zero time until the first request succeeds: a freshly built
// client has performed no activity yet (authentication is lazy). Check
// IsZero to distinguish "never used" from a real timestamp.
func (c *Client) LastActivity() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastActivity
}

// FindResponse is the result of a Find. Records is empty (non-nil) when no
// records match. DataInfo carries the host's record counts.
type FindResponse struct {
	Records  []Record
	DataInfo DataInfo
}

// DataInfo mirrors the "dataInfo" object the host returns with a find result.
type DataInfo struct {
	Database         string `json:"database"`
	Layout           string `json:"layout"`
	Table            string `json:"table"`
	TotalRecordCount int    `json:"totalRecordCount"`
	FoundCount       int    `json:"foundCount"`
	ReturnedCount    int    `json:"returnedCount"`
}

// CreateResponse is the host's acknowledgement of a Create. The client does not
// fetch the new record's field data; issue a Find for that.
type CreateResponse struct {
	RecordID string
	ModID    string
}

// UpdateResponse is the host's acknowledgement of an Update.
type UpdateResponse struct {
	ModID string
}

// Find runs the query against the layout. A query that matches no records
// returns a FindResponse with an empty Records slice and a nil error.
func (c *Client) Find(ctx context.Context, layout string, query Query) (FindResponse, error) {
	if layout == "" {
		return FindResponse{}, errors.New("filemaker: no layout specified")
	}

	body, err := json.Marshal(query)
	if err != nil {
		return FindResponse{}, fmt.Errorf("filemaker: failed to marshal query: %w", err)
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodPost, c.findURL(layout), body, &rb); err != nil {
		if errors.Is(err, ErrNoRecords) {
			return FindResponse{Records: []Record{}}, nil
		}
		return FindResponse{}, err
	}

	records := make([]Record, len(rb.Response.Data))
	for i, w := range rb.Response.Data {
		records[i] = Record{
			id:         w.ID,
			modID:      w.ModID,
			layout:     layout,
			fieldData:  w.FieldData,
			portalData: w.PortalData,
			loc:        c.location,
		}
	}
	return FindResponse{Records: records, DataInfo: rb.Response.DataInfo}, nil
}

// Create inserts a new record with the given field data and returns the host's
// acknowledgement (record ID and mod ID).
func (c *Client) Create(ctx context.Context, layout string, fields FieldData) (CreateResponse, error) {
	if layout == "" {
		return CreateResponse{}, errors.New("filemaker: no layout specified")
	}

	body, err := marshalRecordBody(fields, "")
	if err != nil {
		return CreateResponse{}, err
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodPost, c.recordsURL(layout), body, &rb); err != nil {
		return CreateResponse{}, err
	}
	return CreateResponse{RecordID: rb.Response.RecordID, ModID: rb.Response.ModID}, nil
}

// UpdateOption configures an Update or UpdateByID.
type UpdateOption func(*updateConfig)

// updateConfig holds the optional parameters applied by UpdateOption values.
// Optimistic concurrency is one flag plus a version: conditional means a mod-ID
// check is wanted, and modID is the version to check against (empty means
// "source it from the record"). Both WithModID and IfUnchanged set conditional,
// so the options are order-independent. err carries deferred option validation
// (an UpdateOption cannot return an error directly), surfaced when resolved.
type updateConfig struct {
	conditional bool
	modID       string
	err         error
}

// WithModID makes the update conditional (optimistic concurrency) against a
// specific mod ID: the host rejects it with ErrRecordModified if the record's
// current mod ID differs — i.e. it changed since modID was read. modID must be
// non-empty; an empty one is reported as an error from Update/UpdateByID. To
// lock against the record you are updating, prefer IfUnchanged.
func WithModID(modID string) UpdateOption {
	return func(c *updateConfig) {
		if modID == "" {
			c.err = errors.New("filemaker: WithModID requires a non-empty mod ID")
			return
		}
		c.conditional = true
		c.modID = modID
	}
}

// IfUnchanged makes the update conditional on the record not having changed
// since it was read: it locks against the record's own ModID, so the host
// rejects the write with ErrRecordModified if another writer modified the record
// in the meantime. It is the ergonomic form of WithModID(rec.ModID()).
//
// Only the record-based Update can honor it (UpdateByID has no record to read a
// ModID from, and reports an error); a record without a ModID is likewise an
// error rather than a silent unconditional write. Combining it with WithModID is
// redundant — the explicit version from WithModID is used, regardless of order.
func IfUnchanged() UpdateOption {
	return func(c *updateConfig) {
		c.conditional = true
	}
}

// resolveUpdateConfig applies the options and resolves the mod ID. A conditional
// update with no explicit version sources it from rec (nil for the id-addressed
// path, which cannot honor IfUnchanged). Deferred option errors surface here.
func resolveUpdateConfig(opts []UpdateOption, rec *Record) (updateConfig, error) {
	var cfg updateConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.err != nil {
		return cfg, cfg.err
	}
	if cfg.conditional && cfg.modID == "" {
		switch {
		case rec == nil:
			return cfg, errors.New("filemaker: IfUnchanged requires a record; use WithModID with UpdateByID")
		case rec.modID == "":
			return cfg, errors.New("filemaker: IfUnchanged requires a record with a ModID")
		}
		cfg.modID = rec.modID
	}
	return cfg, nil
}

// Update writes the given field data to the record, identified by rec, and
// returns the new mod ID. fields is a patch: only the named fields are written,
// and the rest of the record is left unchanged on the host. rec is used solely
// to address the record (its Layout and ID); its own field values are not sent.
// Writes are unconditional by default; pass IfUnchanged for optimistic
// concurrency against the record's ModID.
func (c *Client) Update(ctx context.Context, rec Record, fields FieldData, opts ...UpdateOption) (UpdateResponse, error) {
	if rec.id == "" {
		return UpdateResponse{}, errors.New("filemaker: record has no ID; create or find it first")
	}
	// IfUnchanged is record-relative, so resolve it here (UpdateByID has no record
	// to read a ModID from) and append the resolved lock as an explicit WithModID.
	// Every other option passes through untouched, so new UpdateOptions need no
	// change here; delegating also keeps layout/id validation and the write in one
	// place, like Delete and UploadToContainer.
	cfg, err := resolveUpdateConfig(opts, &rec)
	if err != nil {
		return UpdateResponse{}, err
	}
	if cfg.conditional {
		// Full-slice expression so the append never mutates the caller's array.
		opts = append(opts[:len(opts):len(opts)], WithModID(cfg.modID))
	}
	return c.UpdateByID(ctx, rec.layout, rec.id, fields, opts...)
}

// UpdateByID writes the given field data to an existing record addressed by
// layout and id, and returns the new mod ID. See Update for the patch semantics.
// For optimistic concurrency pass WithModID (IfUnchanged needs a record).
func (c *Client) UpdateByID(ctx context.Context, layout, id string, fields FieldData, opts ...UpdateOption) (UpdateResponse, error) {
	switch {
	case layout == "":
		return UpdateResponse{}, errors.New("filemaker: no layout specified")
	case id == "":
		return UpdateResponse{}, errors.New("filemaker: no record id specified")
	}

	cfg, err := resolveUpdateConfig(opts, nil)
	if err != nil {
		return UpdateResponse{}, err
	}

	body, err := marshalRecordBody(fields, cfg.modID)
	if err != nil {
		return UpdateResponse{}, err
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodPatch, c.recordURL(layout, id), body, &rb); err != nil {
		if errors.Is(err, ErrRecordModified) {
			return UpdateResponse{}, fmt.Errorf("filemaker: record %q in layout %q: %w", id, layout, ErrRecordModified)
		}
		return UpdateResponse{}, err
	}
	return UpdateResponse{ModID: rb.Response.ModID}, nil
}

// Delete removes the record identified by rec.
func (c *Client) Delete(ctx context.Context, rec Record) error {
	if rec.id == "" {
		return errors.New("filemaker: record has no ID; create or find it first")
	}
	return c.DeleteByID(ctx, rec.layout, rec.id)
}

// DeleteByID removes a record addressed by layout and id.
func (c *Client) DeleteByID(ctx context.Context, layout, id string) error {
	switch {
	case layout == "":
		return errors.New("filemaker: no layout specified")
	case id == "":
		return errors.New("filemaker: no record id specified")
	}

	var rb responseBody
	return c.do(ctx, http.MethodDelete, c.recordURL(layout, id), nil, &rb)
}

// UploadToContainer uploads data to a container field of the record identified
// by rec. The record must already exist (created or returned by a find).
func (c *Client) UploadToContainer(ctx context.Context, rec Record, field, filename string, data io.Reader) error {
	if rec.id == "" {
		return errors.New("filemaker: record has no ID; create or find it first")
	}
	return c.UploadToContainerByID(ctx, rec.layout, rec.id, field, filename, data)
}

// UploadToContainerByID uploads data to a container field of an existing record
// addressed by layout and id.
func (c *Client) UploadToContainerByID(ctx context.Context, layout, id, field, filename string, data io.Reader) error {
	switch {
	case layout == "":
		return errors.New("filemaker: no layout specified")
	case id == "":
		return errors.New("filemaker: no record id specified")
	case field == "":
		return errors.New("filemaker: no container field specified")
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("upload", filename)
	if err != nil {
		return fmt.Errorf("filemaker: failed to build upload: %w", err)
	}
	if _, err := io.Copy(part, data); err != nil {
		return fmt.Errorf("filemaker: failed to read upload data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("filemaker: failed to finalize upload: %w", err)
	}

	contentType := w.FormDataContentType()
	body := buf.Bytes()
	var rb responseBody
	return c.withAuth(ctx, func() (string, error) {
		return c.attempt(ctx, http.MethodPost, c.containerURL(layout, id, field), contentType, body, &rb)
	})
}

// DownloadFromContainer downloads the binary contents of a container field of
// the record identified by rec. The field must hold a container streaming URL
// (the value FileMaker returns for a container field); an empty or non-string
// field is reported as an error.
func (c *Client) DownloadFromContainer(ctx context.Context, rec Record, field string) ([]byte, error) {
	u, err := rec.StringE(field)
	if err != nil || u == "" {
		return nil, fmt.Errorf("filemaker: field %q is not a container URL", field)
	}
	return c.DownloadFromContainerByURL(ctx, u)
}

// DownloadFromContainerByURL downloads the binary contents of a container from
// its streaming URL. The URL must be on the session host (typically obtained
// from a record via record.String(field)); the bearer token is never sent to a
// foreign host.
func (c *Client) DownloadFromContainerByURL(ctx context.Context, containerURL string) ([]byte, error) {
	if containerURL == "" {
		return nil, errors.New("filemaker: empty container url")
	}
	if !strings.HasPrefix(containerURL, c.host) {
		return nil, fmt.Errorf("filemaker: refusing to fetch container from foreign host: %s", containerURL)
	}

	if err := c.ensureAuthenticated(ctx); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, containerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("filemaker: failed to build request: %w", err)
	}
	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("filemaker: request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("filemaker: failed to fetch container data: %s", res.Status)
	}

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("filemaker: failed to read container data: %w", err)
	}

	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()
	return data, nil
}

// marshalRecordBody wraps fields in the {"fieldData": ...} envelope the host
// expects, optionally including a modId for optimistic locking (omitted when
// empty). A nil map becomes an empty object so the host applies defaults.
func marshalRecordBody(fields FieldData, modID string) ([]byte, error) {
	if fields == nil {
		fields = FieldData{}
	}
	body, err := json.Marshal(struct {
		FieldData FieldData `json:"fieldData"`
		ModID     string    `json:"modId,omitempty"`
	}{fields, modID})
	if err != nil {
		return nil, fmt.Errorf("filemaker: failed to marshal field data: %w", err)
	}
	return body, nil
}

// responseBody mirrors the envelope every Data API call returns. The wire form
// encodes message codes as strings; check() converts them.
type responseBody struct {
	Response struct {
		Token    string       `json:"token"`
		RecordID string       `json:"recordId"`
		ModID    string       `json:"modId"`
		DataInfo DataInfo     `json:"dataInfo"`
		Data     []recordWire `json:"data"`
	} `json:"response"`
	Messages []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"messages"`
}

// recordWire is the wire shape of a "data" item. Record's field and portal maps
// are unexported (so records are immutable), which encoding/json cannot set, so
// the response decodes into this exported-field struct and Find builds Records
// from it.
type recordWire struct {
	ID         string                      `json:"recordId"`
	ModID      string                      `json:"modId"`
	FieldData  map[string]any              `json:"fieldData"`
	PortalData map[string][]map[string]any `json:"portalData"`
}

// check inspects the host messages and returns an *APIError unless the primary
// message reports success (code 0). It guards against an empty messages array
// rather than indexing blindly.
func (rb *responseBody) check() error {
	if len(rb.Messages) == 0 {
		return errors.New("filemaker: response contained no messages")
	}

	if primary, err := strconv.Atoi(rb.Messages[0].Code); err == nil && primary == 0 {
		return nil
	}

	msgs := make([]Message, len(rb.Messages))
	for i, m := range rb.Messages {
		code, _ := strconv.Atoi(m.Code)
		msgs[i] = Message{Code: code, Text: m.Message}
	}
	return &APIError{Messages: msgs}
}

// do is the generic authenticated transport for JSON record operations.
//
// body is the marshaled request payload (nil for GET/DELETE); it is passed as a
// byte slice rather than an io.Reader so it can be replayed on a reauth retry.
func (c *Client) do(ctx context.Context, method, url string, body []byte, out *responseBody) error {
	return c.withAuth(ctx, func() (string, error) {
		return c.attempt(ctx, method, url, "application/json", body, out)
	})
}

// withAuth wraps a request attempt with lazy first-use authentication and
// optional automatic re-authentication.
//
// Lazy: if no session exists yet, authenticate before the attempt. Always runs,
// independent of the reauth options.
//
// Proactive (WithReauthOnIdle): if the session has been idle past the configured
// timeout, refresh the token before the attempt. Best-effort — a failed refresh
// falls through to the attempt, which surfaces any real error.
//
// Reactive (WithReauthOnInvalidToken): if the attempt fails with an
// invalid-token error, re-authenticate and retry once.
//
// Both triggers funnel into the same de-duplicated authenticate, so concurrent
// callers that all detect expiry collapse to a single re-auth. The attempt
// returns the token it used, which is the de-dup key.
func (c *Client) withAuth(ctx context.Context, attempt func() (string, error)) error {
	// Lazy authentication: acquire the initial token on first use. A failed
	// login here is fatal to the attempt, so its error is returned (unlike the
	// best-effort proactive refresh below).
	if err := c.ensureAuthenticated(ctx); err != nil {
		return err
	}

	if c.idleTimeout > 0 && c.idle() {
		c.mu.RLock()
		used := c.token
		c.mu.RUnlock()
		_ = c.authenticate(ctx, used)
	}

	used, err := attempt()

	if c.reauthOnInvalidToken && errors.Is(err, ErrInvalidToken) {
		if rerr := c.authenticate(ctx, used); rerr != nil {
			return rerr
		}
		_, err = attempt()
	}
	return err
}

// idle reports whether the session has been idle at least the configured idle
// timeout.
func (c *Client) idle() bool {
	c.mu.RLock()
	last := c.lastActivity
	c.mu.RUnlock()
	return time.Since(last) >= c.idleTimeout
}

// attempt performs a single authenticated round-trip and stamps lastActivity
// whenever the host responded (success or an API-level error). It returns the
// token it used so the caller can de-duplicate re-authentication.
func (c *Client) attempt(ctx context.Context, method, url, contentType string, body []byte, out *responseBody) (string, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return "", fmt.Errorf("filemaker: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)

	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()
	req.Header.Set("Authorization", "Bearer "+token)

	err = c.send(req, out)

	var apiErr *APIError
	if err == nil || errors.As(err, &apiErr) {
		c.mu.Lock()
		c.lastActivity = time.Now()
		c.mu.Unlock()
	}
	return token, err
}

// send performs one round-trip: it sends req, reads and decodes the response
// into out, and reports any host error. It holds no locks and sets no auth
// headers, so it is shared by both login (Basic auth) and do (Bearer).
func (c *Client) send(req *http.Request, out *responseBody) error {
	res, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("filemaker: request failed: %w", err)
	}
	defer res.Body.Close()

	bodyBytes, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("filemaker: failed to read response body: %w", err)
	}

	if err := json.Unmarshal(bodyBytes, out); err != nil {
		return fmt.Errorf("filemaker: failed to decode response (status %s): %w", res.Status, err)
	}

	return out.check()
}

// login performs a Basic-auth login and returns a fresh session token.
func (c *Client) login(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/sessions", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", fmt.Errorf("filemaker: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Basic "+basicAuth(c.username, c.password))

	var rb responseBody
	if err := c.send(req, &rb); err != nil {
		return "", err
	}
	if rb.Response.Token == "" {
		return "", errors.New("filemaker: authentication response contained no token")
	}
	return rb.Response.Token, nil
}

// authenticate replaces the stored token with one from a fresh login,
// de-duplicating concurrent callers. observedToken is the token the caller last
// saw; if another goroutine already refreshed the token since then, this returns
// without a network round-trip. It serves both first-time and repeat
// authentication — the empty string as observedToken is the first-use key.
//
// reauthSem (a cap-1 channel used as a context-aware mutex) serializes logins so
// concurrent callers collapse to one network call; a caller waiting on it honors
// its ctx and bails on cancellation rather than blocking on an in-flight login.
// The login itself runs without holding the read/write lock, so token reads (and
// LastActivity) stay live during a refresh.
func (c *Client) authenticate(ctx context.Context, observedToken string) error {
	select {
	case c.reauthSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.reauthSem }()

	c.mu.RLock()
	current := c.token
	c.mu.RUnlock()
	if current != observedToken {
		return nil
	}

	token, err := c.login(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.token = token
	c.lastActivity = time.Now()
	c.mu.Unlock()
	return nil
}

// baseURL builds the database-scoped root of the Data API URL.
func (c *Client) baseURL() string {
	return fmt.Sprintf("%s/fmi/data/v1/databases/%s", c.host, c.database)
}

// recordsURL is the collection endpoint for a layout (used to create records).
func (c *Client) recordsURL(layout string) string {
	return fmt.Sprintf("%s/layouts/%s/records", c.baseURL(), layout)
}

// recordURL is the endpoint for a single record by ID.
func (c *Client) recordURL(layout, id string) string {
	return fmt.Sprintf("%s/layouts/%s/records/%s", c.baseURL(), layout, id)
}

// findURL is the find endpoint for a layout.
func (c *Client) findURL(layout string) string {
	return fmt.Sprintf("%s/layouts/%s/_find", c.baseURL(), layout)
}

// containerURL is the upload endpoint for a record's container field.
func (c *Client) containerURL(layout, id, field string) string {
	return fmt.Sprintf("%s/layouts/%s/records/%s/containers/%s", c.baseURL(), layout, id, field)
}

// normalizeHost defaults the scheme to https when none is present, then
// validates it: plaintext http is allowed only when allowInsecureHTTP is set,
// and any scheme other than http or https is rejected outright.
func normalizeHost(host string, allowInsecureHTTP bool) (string, error) {
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}

	u, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("filemaker: invalid host %q: %w", host, err)
	}

	switch u.Scheme {
	case "https":
	case "http":
		if !allowInsecureHTTP {
			return "", errors.New("filemaker: refusing to connect over plaintext http; use https or pass WithInsecureHTTP for development")
		}
	default:
		return "", fmt.Errorf("filemaker: unsupported host scheme %q; use https (or http with WithInsecureHTTP)", u.Scheme)
	}

	return host, nil
}

// basicAuth builds the value for an HTTP Basic Authorization header.
func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}
