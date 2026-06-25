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

	c, err := New(srv.URL, "db", "user", "pass", WithInsecureHTTP())
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

	c, err := New(srv.URL, "db", "user", "pass", WithInsecureHTTP())
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
	// Authenticating a client with no database or username fails fast with a
	// clear error, before any network round-trip.
	if _, err := New("h", "", "u", "p"); err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, tc := range []struct {
		name           string
		db, user, pass string
	}{
		{"empty database", "", "u", "p"},
		{"empty username", "db", "", "p"},
	} {
		c, err := New("https://example.invalid", tc.db, tc.user, tc.pass)
		if err != nil {
			t.Fatalf("%s: New: %v", tc.name, err)
		}
		if err := c.Authenticate(context.Background()); err == nil {
			t.Errorf("%s: Authenticate returned nil, want error", tc.name)
		}
	}
}

func TestAuthenticateError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"212","message":"Invalid credentials"}]}`)
	}))
	defer srv.Close()

	c, err := New(srv.URL, "db", "user", "bad", WithInsecureHTTP())
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

func TestLogoutNoSession(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		writeJSON(w, okSession)
	}))
	defer srv.Close()

	c, err := New(srv.URL, "db", "user", "pass", WithInsecureHTTP())
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
