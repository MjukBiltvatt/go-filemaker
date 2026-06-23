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
	"strconv"
	"strings"
	"sync"
	"time"
)

// codeInvalidToken is the FileMaker error code for an expired/invalid session
// token. It drives the optional auto-reauth retry.
const codeInvalidToken = 952

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

	cfg := config{timeout: 30 * time.Second}
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

// responseBody mirrors the envelope every Data API call returns. The wire form
// encodes message codes as strings; check() converts them.
type responseBody struct {
	Response struct {
		Token    string `json:"token"`
		RecordID string `json:"recordId"`
		ModID    string `json:"modId"`
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

// do is the generic authenticated transport for record operations. It sends the
// request with the current bearer token and, when WithAutoReauth is enabled,
// re-authenticates once and retries on an expired-token error.
//
// body is the marshaled request payload (nil for GET/DELETE); it is passed as a
// byte slice rather than an io.Reader so it can be replayed on retry.
func (c *Client) do(ctx context.Context, method, url string, body []byte, out *responseBody) error {
	err := c.attempt(ctx, method, url, body, out)

	var apiErr *APIError
	if c.autoReauth && errors.As(err, &apiErr) && apiErr.Code() == codeInvalidToken {
		if rerr := c.reauthenticate(ctx); rerr != nil {
			return rerr
		}
		return c.attempt(ctx, method, url, body, out)
	}
	return err
}

// attempt performs a single authenticated round-trip and stamps lastActivity
// whenever the host responded (success or an API-level error).
func (c *Client) attempt(ctx context.Context, method, url string, body []byte, out *responseBody) error {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, r)
	if err != nil {
		return fmt.Errorf("filemaker: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

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
