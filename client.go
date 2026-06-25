package filemaker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

	authSem      chan struct{} // cap-1 channel used as a context-aware mutex serializing logins
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
// Only host is required here. database and username are validated when a session
// is actually established (see login), so a client may be built with neither to
// reach the unauthenticated, database-agnostic ProductInfo endpoint; any
// authenticated or database-scoped operation then fails fast with a clear error.
//
// The host may include a scheme; if it does not, https is assumed. Plaintext
// http is rejected unless WithInsecureHTTP is passed, and any scheme other than
// http or https is rejected outright.
func New(host, database, username, password string, opts ...Option) (*Client, error) {
	if host == "" {
		return nil, errors.New("filemaker: no host specified")
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
		authSem:              make(chan struct{}, 1),
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

// responseBody mirrors the envelope every Data API call returns. The wire form
// encodes message codes as strings; check() converts them.
type responseBody struct {
	Response struct {
		Token       string       `json:"token"`
		RecordID    string       `json:"recordId"`
		ModID       string       `json:"modId"`
		DataInfo    DataInfo     `json:"dataInfo"`
		Data        []recordWire `json:"data"`
		ProductInfo ProductInfo  `json:"productInfo"`
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

// login performs a Basic-auth login and returns a fresh session token. The
// session is database-scoped, so both database and username are required here;
// New defers their validation to this point so a credential-free client can
// still reach the unauthenticated ProductInfo endpoint.
func (c *Client) login(ctx context.Context) (string, error) {
	switch {
	case c.database == "":
		return "", errors.New("filemaker: no database specified")
	case c.username == "":
		return "", errors.New("filemaker: no username specified")
	}

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
// authSem (a cap-1 channel used as a context-aware mutex) serializes logins so
// concurrent callers collapse to one network call; a caller waiting on it honors
// its ctx and bails on cancellation rather than blocking on an in-flight login.
// The login itself runs without holding the read/write lock, so token reads (and
// LastActivity) stay live during a refresh.
func (c *Client) authenticate(ctx context.Context, observedToken string) error {
	select {
	case c.authSem <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-c.authSem }()

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

// apiURL builds the version-scoped root of the Data API URL, above the database
// scope. Host-level metadata endpoints such as productInfo hang off it directly
// rather than under a database.
func (c *Client) apiURL() string {
	return c.host + "/fmi/data/v1"
}

// baseURL builds the database-scoped root of the Data API URL.
func (c *Client) baseURL() string {
	return fmt.Sprintf("%s/databases/%s", c.apiURL(), c.database)
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
