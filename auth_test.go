package filemaker

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthenticate(t *testing.T) {
	var mu sync.Mutex
	var gotAuth, gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth, gotPath, gotMethod = r.Header.Get("Authorization"), r.URL.Path, r.Method
		mu.Unlock()
		writeJSON(w, okSession)
	}))
	defer srv.Close()

	c, err := New(srv.URL, "db", BasicAuth("user", "pass"), WithInsecureHTTP())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if c.token != "tok" {
		t.Errorf("token = %q, want tok", c.token)
	}

	mu.Lock()
	auth, path, method := gotAuth, gotPath, gotMethod
	mu.Unlock()

	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if auth != wantAuth {
		t.Errorf("auth = %q, want %q", auth, wantAuth)
	}
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
	if !strings.HasSuffix(path, "/databases/db/sessions") {
		t.Errorf("path = %q, want suffix /databases/db/sessions", path)
	}
}

func TestLazyAuthOnFirstUse(t *testing.T) {
	var sessionCalls, opCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sessions") {
			sessionCalls.Add(1)
			writeJSON(w, okSession)
			return
		}
		opCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("op auth = %q, want Bearer tok (lazy auth should run first)", got)
		}
		writeJSON(w, `{"response":{"recordId":"1"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c, err := New(srv.URL, "db", BasicAuth("user", "pass"), WithInsecureHTTP())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	var rb responseBody
	if err := c.do(context.Background(), http.MethodGet, c.baseURL()+"/x", nil, &rb); err != nil {
		t.Fatalf("do: %v", err)
	}
	if n := sessionCalls.Load(); n != 1 {
		t.Errorf("session calls = %d, want 1 (lazy auth)", n)
	}
	if n := opCalls.Load(); n != 1 {
		t.Errorf("op calls = %d, want 1", n)
	}
	if c.token != "tok" {
		t.Errorf("token = %q, want tok", c.token)
	}
}

func TestLoginValidation(t *testing.T) {
	// Authenticating a client with no database or no credentials fails fast with
	// a clear error, before any network round-trip. New allows both, so a client
	// can still reach the unauthenticated ProductInfo endpoint.
	for _, tc := range []struct {
		name  string
		db    string
		creds Credentials
		want  string
	}{
		{"empty database", "", BasicAuth("u", "p"), "no database specified"},
		{"nil credentials", "db", nil, "no credentials specified"},
	} {
		c, err := New("https://example.invalid", tc.db, tc.creds)
		if err != nil {
			t.Fatalf("%s: New: %v", tc.name, err)
		}
		if err := c.Authenticate(context.Background()); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: Authenticate = %v, want error containing %q", tc.name, err, tc.want)
		}
	}
}

func TestBasicAuthValidation(t *testing.T) {
	// A BasicAuth with no username can never log in, so New rejects it rather
	// than deferring the failure to the first operation.
	_, err := New("h", "db", BasicAuth("", "p"))
	if err == nil || !strings.Contains(err.Error(), "no username specified") {
		t.Errorf("New with empty username = %v, want error containing %q", err, "no username specified")
	}
}

// countingCredentials counts how often the client presents its credentials and
// fails with err when set, standing in for a login method whose credential is
// obtained per login.
type countingCredentials struct {
	calls atomic.Int32
	err   error
}

func (c *countingCredentials) validate() error { return nil }

func (c *countingCredentials) authorize(_ context.Context, h http.Header) error {
	c.calls.Add(1)
	if c.err != nil {
		return c.err
	}
	h.Set("Authorization", "Test")
	return nil
}

func TestCredentialsPresentedPerLogin(t *testing.T) {
	// Every login asks the credentials for its headers afresh, re-logins
	// included, so a credential that expires can be renewed between them.
	var gotAuth atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth.Store(r.Header.Get("Authorization"))
		writeJSON(w, okSession)
	}))
	defer srv.Close()

	creds := &countingCredentials{}
	c, err := New(srv.URL, "db", creds, WithInsecureHTTP())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for i := 0; i < 2; i++ {
		if err := c.Authenticate(context.Background()); err != nil {
			t.Fatalf("Authenticate: %v", err)
		}
	}
	if n := creds.calls.Load(); n != 2 {
		t.Errorf("credentials presented %d times, want 2 (once per login)", n)
	}
	if got := gotAuth.Load(); got != "Test" {
		t.Errorf("login auth = %v, want the header the credentials set", got)
	}
}

func TestCredentialsErrorAbortsLogin(t *testing.T) {
	// Credentials that cannot be presented fail the login before any request is
	// sent, and the caller sees their error unchanged.
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		writeJSON(w, okSession)
	}))
	defer srv.Close()

	wantErr := errors.New("token unavailable")
	c, err := New(srv.URL, "db", &countingCredentials{err: wantErr}, WithInsecureHTTP())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Authenticate(context.Background()); !errors.Is(err, wantErr) {
		t.Errorf("Authenticate = %v, want %v", err, wantErr)
	}
	if _, err := c.Databases(context.Background()); !errors.Is(err, wantErr) {
		t.Errorf("Databases = %v, want %v", err, wantErr)
	}
	if n := requests.Load(); n != 0 {
		t.Errorf("requests sent = %d, want 0", n)
	}
	if c.token != "" {
		t.Errorf("token = %q, want none after a failed login", c.token)
	}
}

func TestAuthenticateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"212","message":"Invalid credentials"}]}`)
	}))
	defer srv.Close()

	c, err := New(srv.URL, "db", BasicAuth("user", "bad"), WithInsecureHTTP())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	err = c.Authenticate(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code() != 212 {
		t.Fatalf("got %v, want *APIError with code 212", err)
	}
}

func TestLogout(t *testing.T) {
	var mu sync.Mutex
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath, gotMethod = r.URL.Path, r.Method
		mu.Unlock()
		writeJSON(w, `{"response":{},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if err := c.Logout(context.Background()); err != nil {
		t.Fatalf("Logout: %v", err)
	}

	mu.Lock()
	path, method := gotPath, gotMethod
	mu.Unlock()
	if method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", method)
	}
	if !strings.HasSuffix(path, "/sessions/tok") {
		t.Errorf("path = %q, want suffix /sessions/tok", path)
	}
	if c.token != "" {
		t.Errorf("token = %q, want cleared", c.token)
	}
}

// TestLogoutKeepsConcurrentRefreshToken pins the ordering that makes Logout's
// write unsafe: a re-authentication that completes while the logout DELETE is
// still in flight must survive. Logout invalidates the token concurrent
// operations are still using, so it provokes the 952 that drives the reactive
// refresh — discarding that fresh token would leave a live session stranded on
// the host with nothing left to log it out.
//
// The handshake is explicit rather than timing-based: the DELETE handler parks
// until released, which is the only way to hold Logout between its token read
// and its write. Every wait is bounded so a regression fails the test instead of
// hanging it.
func TestLogoutKeepsConcurrentRefreshToken(t *testing.T) {
	deleteReceived := make(chan struct{})
	releaseDelete := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodDelete:
			close(deleteReceived)
			<-releaseDelete
			writeJSON(w, `{"response":{},"messages":[{"code":"0","message":"OK"}]}`)
		case http.MethodPost:
			writeJSON(w, `{"response":{"token":"fresh"},"messages":[{"code":"0","message":"OK"}]}`)
		default:
			t.Errorf("unexpected %s request", r.Method)
		}
	}))
	defer srv.Close()

	c := testClient(srv) // seeded with token "tok"

	logoutDone := make(chan error, 1)
	go func() { logoutDone <- c.Logout(context.Background()) }()

	select {
	case <-deleteReceived:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the logout DELETE")
	}

	// Logout is now parked past its token read. Stand in for the reactive
	// refresh an in-flight operation would perform after its 952.
	if _, err := c.authenticate(context.Background(), "tok"); err != nil {
		t.Fatalf("authenticate: %v", err)
	}

	close(releaseDelete)
	select {
	case err := <-logoutDone:
		if err != nil {
			t.Fatalf("Logout: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for Logout to return")
	}

	c.mu.RLock()
	got := c.token
	c.mu.RUnlock()
	if got != "fresh" {
		t.Errorf("token = %q, want %q: Logout discarded a token installed while its DELETE was in flight", got, "fresh")
	}
}

func TestLogoutNoSession(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		writeJSON(w, okSession)
	}))
	defer srv.Close()

	c, err := New(srv.URL, "db", BasicAuth("user", "pass"), WithInsecureHTTP())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Logout(context.Background()); err != nil {
		t.Fatalf("Logout: %v", err)
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("Logout made %d HTTP calls, want 0 (no session)", n)
	}
}
