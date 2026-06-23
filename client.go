package filemaker

import (
	"net/http"
	"sync"
	"time"
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

// LastActivity returns the time of the last successful request made with the
// client. It defaults to the time the session was created until another request
// is made.
func (c *Client) LastActivity() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastActivity
}
