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
	"strconv"
	"strings"
	"sync"
	"time"
)

// FileMaker error codes the client interprets specially.
const (
	codeNoRecords    = 401 // no records match a find request
	codeInvalidToken = 952 // expired/invalid session token; drives auto-reauth
)

// Client is a handle to a FileMaker Data API session. It is safe for concurrent
// use by multiple goroutines: the session token and last-activity timestamp are
// guarded by an internal RWMutex, and the underlying *http.Client is itself
// concurrency-safe.
//
// Records returned by the client are plain data carriers; all operations that
// touch the host are methods on the Client.
type Client struct {
	httpClient *http.Client
	host       string
	database   string
	username   string
	password   string
	autoReauth bool
	location   *time.Location

	mu           sync.RWMutex
	token        string
	lastActivity time.Time
}

// Option configures a Client created with New.
type Option func(*config)

// config holds the optional parameters applied by Option values in New.
type config struct {
	timeout    time.Duration
	autoReauth bool
	location   *time.Location
}

// WithTimeout sets the timeout applied to every HTTP request made by the
// client. The default is 30 seconds; a timeout of 0 disables it entirely.
func WithTimeout(timeout time.Duration) Option {
	return func(c *config) {
		c.timeout = timeout
	}
}

// WithAutoReauth enables transparent re-authentication: when a request fails
// because the session token has expired, the client re-authenticates once and
// retries the request. Disabled by default, in which case an expired token
// surfaces as an error to the caller.
func WithAutoReauth() Option {
	return func(c *config) {
		c.autoReauth = true
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

// New starts a database session by authenticating against the host. The host
// may include a scheme; if it does not, https is assumed.
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

	jar, _ := cookiejar.New(nil)
	c := &Client{
		httpClient: &http.Client{Timeout: cfg.timeout, Jar: jar},
		host:       normalizeHost(host),
		database:   database,
		username:   username,
		password:   password,
		autoReauth: cfg.autoReauth,
		location:   cfg.location,
	}

	token, err := c.authenticate(context.Background())
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	c.token = token
	c.lastActivity = time.Now()
	c.mu.Unlock()

	return c, nil
}

// Destroy logs out of the database session, invalidating the token.
func (c *Client) Destroy(ctx context.Context) error {
	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()

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
// client. It defaults to the time the session was created until another request
// is made.
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
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.Code() == codeNoRecords {
			return FindResponse{Records: []Record{}}, nil
		}
		return FindResponse{}, err
	}

	records := rb.Response.Data
	for i := range records {
		records[i].Layout = layout
		records[i].loc = c.location
	}
	return FindResponse{Records: records, DataInfo: rb.Response.DataInfo}, nil
}

// Create inserts a new record with the given field data and returns the host's
// acknowledgement (record ID and mod ID).
func (c *Client) Create(ctx context.Context, layout string, fields FieldData) (CreateResponse, error) {
	if layout == "" {
		return CreateResponse{}, errors.New("filemaker: no layout specified")
	}

	body, err := marshalFieldData(fields)
	if err != nil {
		return CreateResponse{}, err
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodPost, c.recordsURL(layout), body, &rb); err != nil {
		return CreateResponse{}, err
	}
	return CreateResponse{RecordID: rb.Response.RecordID, ModID: rb.Response.ModID}, nil
}

// Update writes the given field data to an existing record and returns the new
// mod ID.
func (c *Client) Update(ctx context.Context, layout, id string, fields FieldData) (UpdateResponse, error) {
	switch {
	case layout == "":
		return UpdateResponse{}, errors.New("filemaker: no layout specified")
	case id == "":
		return UpdateResponse{}, errors.New("filemaker: no record id specified")
	}

	body, err := marshalFieldData(fields)
	if err != nil {
		return UpdateResponse{}, err
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodPatch, c.recordURL(layout, id), body, &rb); err != nil {
		return UpdateResponse{}, err
	}
	return UpdateResponse{ModID: rb.Response.ModID}, nil
}

// Delete removes a record by its internal ID.
func (c *Client) Delete(ctx context.Context, layout, id string) error {
	switch {
	case layout == "":
		return errors.New("filemaker: no layout specified")
	case id == "":
		return errors.New("filemaker: no record id specified")
	}

	var rb responseBody
	return c.do(ctx, http.MethodDelete, c.recordURL(layout, id), nil, &rb)
}

// UploadToContainer uploads data to a container field of an existing record. The
// record must already exist (created or returned by a find).
func (c *Client) UploadToContainer(ctx context.Context, layout, id, field, filename string, data io.Reader) error {
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
	return c.withReauth(ctx, func() error {
		return c.attempt(ctx, http.MethodPost, c.containerURL(layout, id, field), contentType, body, &rb)
	})
}

// ContainerData downloads the binary contents of a record's container field. The
// field value must be a container streaming URL on the session host; the bearer
// token is never sent to a foreign host.
func (c *Client) ContainerData(ctx context.Context, record Record, field string) ([]byte, error) {
	raw, ok := record.FieldData[field]
	if !ok {
		return nil, fmt.Errorf("filemaker: field %q not present in record", field)
	}
	uri, ok := raw.(string)
	if !ok || uri == "" {
		return nil, fmt.Errorf("filemaker: field %q is not a container reference", field)
	}
	if !strings.HasPrefix(uri, c.host) {
		return nil, fmt.Errorf("filemaker: refusing to fetch container from foreign host: %s", uri)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
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

// marshalFieldData wraps fields in the {"fieldData": ...} envelope the host
// expects. A nil map becomes an empty object so the host creates defaults.
func marshalFieldData(fields FieldData) ([]byte, error) {
	if fields == nil {
		fields = FieldData{}
	}
	body, err := json.Marshal(struct {
		FieldData FieldData `json:"fieldData"`
	}{fields})
	if err != nil {
		return nil, fmt.Errorf("filemaker: failed to marshal field data: %w", err)
	}
	return body, nil
}

// responseBody mirrors the envelope every Data API call returns. The wire form
// encodes message codes as strings; check() converts them.
type responseBody struct {
	Response struct {
		Token    string   `json:"token"`
		RecordID string   `json:"recordId"`
		ModID    string   `json:"modId"`
		DataInfo DataInfo `json:"dataInfo"`
		Data     []Record `json:"data"`
	} `json:"response"`
	Messages []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"messages"`
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
// byte slice rather than an io.Reader so it can be replayed on an auto-reauth
// retry.
func (c *Client) do(ctx context.Context, method, url string, body []byte, out *responseBody) error {
	return c.withReauth(ctx, func() error {
		return c.attempt(ctx, method, url, "application/json", body, out)
	})
}

// withReauth runs attempt and, when WithAutoReauth is enabled, re-authenticates
// once and retries on an expired-token error.
func (c *Client) withReauth(ctx context.Context, attempt func() error) error {
	err := attempt()

	var apiErr *APIError
	if c.autoReauth && errors.As(err, &apiErr) && apiErr.Code() == codeInvalidToken {
		if rerr := c.reauthenticate(ctx); rerr != nil {
			return rerr
		}
		return attempt()
	}
	return err
}

// attempt performs a single authenticated round-trip and stamps lastActivity
// whenever the host responded (success or an API-level error).
func (c *Client) attempt(ctx context.Context, method, url, contentType string, body []byte, out *responseBody) error {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return fmt.Errorf("filemaker: failed to build request: %w", err)
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
	return err
}

// send performs one round-trip: it sends req, reads and decodes the response
// into out, and reports any host error. It holds no locks and sets no auth
// headers, so it is shared by both authenticate (Basic auth) and do (Bearer).
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

// authenticate performs a Basic-auth login and returns a fresh session token.
func (c *Client) authenticate(ctx context.Context) (string, error) {
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

// reauthenticate replaces the stored token with a freshly authenticated one.
//
// Concurrent callers that all hit an expired token may each re-authenticate
// here; de-duplicating that is left to phase 5 (concurrency hardening). It is
// race-free regardless: the network call happens without holding the lock, and
// only the token swap is guarded.
func (c *Client) reauthenticate(ctx context.Context) error {
	token, err := c.authenticate(ctx)
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

// normalizeHost defaults the scheme to https only when none is present, leaving
// an explicit scheme (including http, used in tests/dev) untouched.
func normalizeHost(host string) string {
	if strings.Contains(host, "://") {
		return host
	}
	return "https://" + host
}

// basicAuth builds the value for an HTTP Basic Authorization header.
func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}
