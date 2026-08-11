package filemaker

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// Authenticate eagerly establishes a session by logging in to the host, so a
// caller can surface credential or connectivity errors at a chosen point rather
// than on the first operation. It is optional: every operation authenticates
// lazily on first use when no session exists yet.
//
// Calling it performs a fresh login and replaces the stored token rather than
// reusing the current session. It does not log out the existing session first:
// the previous token is abandoned, not invalidated, and lingers on the host
// until it times out. Call Logout before Authenticate if you need the old
// session torn down promptly.
//
// Concurrent calls collapse into a single login: a caller that arrives while
// another login is in flight adopts the token that login produces instead of
// opening a session of its own, so a burst of callers does not produce a burst
// of sessions.
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
//
// Logout is not a barrier against concurrent use. It ends the session it
// observed, and an operation running alongside it re-authenticates like any
// other — so a caller that needs the client to be left without a session must
// stop issuing requests before logging out. A refresh that lands while the
// logout is in flight is kept rather than discarded, since abandoning it would
// strand a live session on the host.
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
	// Clear only the token this call logged out, mirroring authenticate's
	// compare-then-write. A concurrent refresh may have installed a newer token
	// while the DELETE was in flight; overwriting it would strand that live
	// session on the host with nothing left to log it out.
	if c.token == token {
		c.token = ""
	}
	// Stamped unconditionally: the host responded, which is what attempt treats
	// as activity, and it belongs to whichever session is now current.
	c.lastActivity = time.Now()
	c.mu.Unlock()
	return nil
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

// basicAuth builds the value for an HTTP Basic Authorization header.
func basicAuth(username, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(username + ":" + password))
}
