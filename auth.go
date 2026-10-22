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
	_, err := c.authenticate(ctx, used)
	return err
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
	_, err := c.authenticate(ctx, "")
	return err
}

// refreshIdle replaces a session that has sat idle past the client's idle
// timeout with a fresh one, then logs the old session out. The idle timeout is
// meant to sit under the host's, so the old session is usually still alive;
// abandoning it would leave it holding one of the host's concurrent sessions
// until the host times it out, and with a short timeout each idle period would
// strand another.
//
// Only the caller whose login replaced the token logs the old one out, so a
// burst of callers arriving after the same idle period sends one DELETE. Both
// steps are best-effort: a failed refresh falls through to the attempt, which
// surfaces any real error, and a failed logout (typically 952 because the old
// session had already expired) leaves nothing more to do.
func (c *Client) refreshIdle(ctx context.Context) {
	c.mu.RLock()
	used := c.token
	c.mu.RUnlock()
	if replaced, err := c.authenticate(ctx, used); err == nil && replaced != "" {
		_ = c.endSession(ctx, replaced)
	}
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
	if err := c.endSession(ctx, token); err != nil {
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

// endSession logs out the session identified by token on the host. It leaves
// the client's stored token alone; the caller decides what that should be.
func (c *Client) endSession(ctx context.Context, token string) error {
	// Logout identifies the session by the token in the URL and takes no
	// Authorization header, so it bypasses the authed do() path.
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.baseURL()+"/sessions/"+token, nil)
	if err != nil {
		return fmt.Errorf("filemaker: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	var rb responseBody
	return c.send(req, &rb)
}

// login opens a session with the client's credentials and returns its token.
// The session is database-scoped, so a database and credentials are both
// required here; New defers that check to this point so a credential-free
// client can still reach the unauthenticated ProductInfo endpoint.
//
// Every login method shares this request — the same endpoint and empty JSON
// body — and differs only in the headers the credentials set on it.
func (c *Client) login(ctx context.Context) (string, error) {
	switch {
	case c.database == "":
		return "", errors.New("filemaker: no database specified")
	case c.credentials == nil:
		return "", errors.New("filemaker: no credentials specified")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL()+"/sessions", bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", fmt.Errorf("filemaker: failed to build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if err := c.credentials.authorize(ctx, req.Header); err != nil {
		return "", err
	}

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
// It returns the token its login replaced, which is empty when it did not log
// in (another caller already had) or when there was no session before.
//
// authSem (a cap-1 channel used as a context-aware mutex) serializes logins so
// concurrent callers collapse to one network call; a caller waiting on it honors
// its ctx and bails on cancellation rather than blocking on an in-flight login.
// The login itself runs without holding the read/write lock, so token reads (and
// LastActivity) stay live during a refresh.
func (c *Client) authenticate(ctx context.Context, observedToken string) (replaced string, err error) {
	select {
	case c.authSem <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-c.authSem }()

	c.mu.RLock()
	current := c.token
	c.mu.RUnlock()
	if current != observedToken {
		return "", nil
	}

	token, err := c.login(ctx)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	replaced = c.token
	c.token = token
	c.lastActivity = time.Now()
	c.mu.Unlock()
	return replaced, nil
}

// Credentials is how a client proves who it is when it opens a session. Build
// one with BasicAuth and pass it to New; pass nil instead for a client that only
// calls the endpoints that need no account, such as ProductInfo.
//
// The client presents its credentials on every login — the first, and each
// re-authentication after the session expires — and to Databases, which takes
// account credentials rather than a session token.
//
// The interface is sealed: the login methods the Data API accepts are a fixed
// set, and each has its constructor in this package.
type Credentials interface {
	// validate reports whether the credentials are complete enough to log in
	// with. New calls it, so an incomplete value fails at construction rather
	// than on first use.
	validate() error

	// authorize sets the headers that carry the credentials on a request. It
	// runs once per request, with that request's context, so a credential that
	// has to be fetched or renewed can be obtained at that point.
	authorize(ctx context.Context, h http.Header) error
}

// BasicAuth returns credentials that log in with a FileMaker account name and
// password, sent as HTTP Basic authentication. username must not be empty.
func BasicAuth(username, password string) Credentials {
	return basicAuth{username: username, password: password}
}

// basicAuth is the Credentials returned by BasicAuth.
type basicAuth struct {
	username, password string
}

func (b basicAuth) validate() error {
	if b.username == "" {
		return errors.New("filemaker: no username specified")
	}
	return nil
}

func (b basicAuth) authorize(_ context.Context, h http.Header) error {
	h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(b.username+":"+b.password)))
	return nil
}
