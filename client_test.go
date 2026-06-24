package filemaker

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const okSession = `{"response":{"token":"tok"},"messages":[{"code":"0","message":"OK"}]}`

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, body)
}

// testClient builds a Client pointed at srv with a known token, bypassing New.
func testClient(srv *httptest.Server) *Client {
	return &Client{
		httpClient: srv.Client(),
		host:       srv.URL,
		database:   "db",
		username:   "user",
		password:   "pass",
		token:      "tok",
		reauthSem:  make(chan struct{}, 1),
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"my.host.com":           "https://my.host.com",
		"https://my.host.com":   "https://my.host.com",
		"http://localhost:8080": "http://localhost:8080",
	}
	for in, want := range cases {
		if got := normalizeHost(in); got != want {
			t.Errorf("normalizeHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNew(t *testing.T) {
	var mu sync.Mutex
	var gotAuth, gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth, gotPath, gotMethod = r.Header.Get("Authorization"), r.URL.Path, r.Method
		mu.Unlock()
		writeJSON(w, okSession)
	}))
	defer srv.Close()

	c, err := New(srv.URL, "db", "user", "pass")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.token != "tok" {
		t.Errorf("token = %q, want tok", c.token)
	}
	if c.LastActivity().IsZero() {
		t.Error("lastActivity not set")
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

func TestNewValidation(t *testing.T) {
	if _, err := New("", "db", "u", "p"); err == nil {
		t.Error("expected error for empty host")
	}
	if _, err := New("h", "", "u", "p"); err == nil {
		t.Error("expected error for empty database")
	}
	if _, err := New("h", "db", "", "p"); err == nil {
		t.Error("expected error for empty username")
	}
}

func TestNewHostError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"212","message":"Invalid credentials"}]}`)
	}))
	defer srv.Close()

	_, err := New(srv.URL, "db", "user", "bad")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code() != 212 {
		t.Fatalf("got %v, want *APIError with code 212", err)
	}
}

func TestDoEmptyMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	var rb responseBody
	err := c.do(context.Background(), http.MethodGet, c.baseURL()+"/x", nil, &rb)
	if err == nil {
		t.Fatal("expected error for empty messages array, got nil")
	}
}

func TestDoMalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `not json`)
	}))
	defer srv.Close()

	c := testClient(srv)
	var rb responseBody
	if err := c.do(context.Background(), http.MethodGet, c.baseURL()+"/x", nil, &rb); err == nil {
		t.Fatal("expected decode error, got nil")
	}
}

func TestDoBearerAndActivity(t *testing.T) {
	var mu sync.Mutex
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		writeJSON(w, `{"response":{"recordId":"1"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	before := c.LastActivity()
	var rb responseBody
	if err := c.do(context.Background(), http.MethodGet, c.baseURL()+"/x", nil, &rb); err != nil {
		t.Fatalf("do: %v", err)
	}

	mu.Lock()
	auth := gotAuth
	mu.Unlock()
	if auth != "Bearer tok" {
		t.Errorf("auth = %q, want Bearer tok", auth)
	}
	if !c.LastActivity().After(before) {
		t.Error("lastActivity not advanced")
	}
}

func TestDoNoRecordsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"401","message":"No records match the request"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	var rb responseBody
	err := c.do(context.Background(), http.MethodGet, c.baseURL()+"/x", nil, &rb)
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code() != 401 {
		t.Fatalf("got %v, want *APIError with code 401", err)
	}
}

func TestDoReauthEnabled(t *testing.T) {
	var opCalls, sessionCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sessions") {
			sessionCalls.Add(1)
			writeJSON(w, `{"response":{"token":"newtok"},"messages":[{"code":"0","message":"OK"}]}`)
			return
		}
		if opCalls.Add(1) == 1 {
			writeJSON(w, `{"response":{},"messages":[{"code":"952","message":"Invalid FileMaker Data API token"}]}`)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer newtok" {
			t.Errorf("retry auth = %q, want Bearer newtok", got)
		}
		writeJSON(w, `{"response":{"recordId":"1"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	c.reauthOnInvalidToken = true
	var rb responseBody
	if err := c.do(context.Background(), http.MethodGet, c.baseURL()+"/x", nil, &rb); err != nil {
		t.Fatalf("do: %v", err)
	}
	if n := opCalls.Load(); n != 2 {
		t.Errorf("op calls = %d, want 2", n)
	}
	if n := sessionCalls.Load(); n != 1 {
		t.Errorf("session calls = %d, want 1", n)
	}
	if c.token != "newtok" {
		t.Errorf("token = %q, want newtok", c.token)
	}
}

func TestDoReauthDisabled(t *testing.T) {
	var opCalls, sessionCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sessions") {
			sessionCalls.Add(1)
			writeJSON(w, okSession)
			return
		}
		opCalls.Add(1)
		writeJSON(w, `{"response":{},"messages":[{"code":"952","message":"Invalid FileMaker Data API token"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv) // reauthOnInvalidToken defaults to false
	var rb responseBody
	err := c.do(context.Background(), http.MethodGet, c.baseURL()+"/x", nil, &rb)

	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code() != 952 {
		t.Fatalf("got %v, want *APIError with code 952", err)
	}
	if n := opCalls.Load(); n != 1 {
		t.Errorf("op calls = %d, want 1", n)
	}
	if n := sessionCalls.Load(); n != 0 {
		t.Errorf("session calls = %d, want 0 (no reauth)", n)
	}
}

func TestReauthDedup(t *testing.T) {
	const n = 10
	var oldTokenHits, opCalls, sessionCalls atomic.Int32
	gate := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sessions") {
			sessionCalls.Add(1)
			writeJSON(w, `{"response":{"token":"newtok"},"messages":[{"code":"0","message":"OK"}]}`)
			return
		}
		opCalls.Add(1)
		if r.Header.Get("Authorization") == "Bearer tok" {
			// Hold every initial (stale-token) request until all n have arrived,
			// so they all hit 952 together — maximal de-dup pressure.
			if oldTokenHits.Add(1) == n {
				close(gate)
			}
			<-gate
			writeJSON(w, `{"response":{},"messages":[{"code":"952","message":"Invalid FileMaker Data API token"}]}`)
			return
		}
		writeJSON(w, `{"response":{"recordId":"1"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	c.reauthOnInvalidToken = true

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var rb responseBody
			if err := c.do(context.Background(), http.MethodGet, c.baseURL()+"/x", nil, &rb); err != nil {
				t.Errorf("do: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := sessionCalls.Load(); got != 1 {
		t.Errorf("session (reauth) calls = %d, want exactly 1", got)
	}
	if got := opCalls.Load(); got != 2*n {
		t.Errorf("op calls = %d, want %d (n failed + n retried)", got, 2*n)
	}
	if c.token != "newtok" {
		t.Errorf("token = %q, want newtok", c.token)
	}
}

func TestProactiveReauthOnIdle(t *testing.T) {
	var opCalls, sessionCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sessions") {
			sessionCalls.Add(1)
			writeJSON(w, `{"response":{"token":"newtok"},"messages":[{"code":"0","message":"OK"}]}`)
			return
		}
		opCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer newtok" {
			t.Errorf("op auth = %q, want Bearer newtok (token should be refreshed before send)", got)
		}
		writeJSON(w, `{"response":{"recordId":"1"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	c.idleTimeout = time.Minute
	c.lastActivity = time.Now().Add(-2 * time.Minute) // idle past the threshold

	var rb responseBody
	if err := c.do(context.Background(), http.MethodGet, c.baseURL()+"/x", nil, &rb); err != nil {
		t.Fatalf("do: %v", err)
	}

	if got := sessionCalls.Load(); got != 1 {
		t.Errorf("session (reauth) calls = %d, want 1 (proactive refresh)", got)
	}
	if got := opCalls.Load(); got != 1 {
		t.Errorf("op calls = %d, want 1 (no doomed request)", got)
	}
	if c.token != "newtok" {
		t.Errorf("token = %q, want newtok", c.token)
	}
}

func TestReauthWaitHonorsContext(t *testing.T) {
	authStarted := make(chan struct{})
	releaseAuth := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(authStarted) // the (only) auth request has begun
		<-releaseAuth      // simulate a slow auth round-trip
		writeJSON(w, `{"response":{"token":"newtok"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)

	// Leader: holds the reauth lock and blocks inside the slow auth.
	leaderDone := make(chan error, 1)
	go func() {
		leaderDone <- c.reauthenticate(context.Background(), "tok")
	}()
	<-authStarted

	// Follower with an already-cancelled ctx must bail at its deadline rather
	// than wait for the blocked leader.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.reauthenticate(ctx, "tok"); !errors.Is(err, context.Canceled) {
		t.Errorf("follower reauth = %v, want context.Canceled", err)
	}

	close(releaseAuth)
	if err := <-leaderDone; err != nil {
		t.Errorf("leader reauth: %v", err)
	}
	if c.token != "newtok" {
		t.Errorf("token = %q, want newtok", c.token)
	}
}

func TestProactiveReauthSkippedWhenActive(t *testing.T) {
	var sessionCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sessions") {
			sessionCalls.Add(1)
		}
		writeJSON(w, `{"response":{"recordId":"1"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	c.idleTimeout = time.Minute
	c.lastActivity = time.Now() // recently active

	var rb responseBody
	if err := c.do(context.Background(), http.MethodGet, c.baseURL()+"/x", nil, &rb); err != nil {
		t.Fatalf("do: %v", err)
	}
	if got := sessionCalls.Load(); got != 0 {
		t.Errorf("session calls = %d, want 0 (not idle, no proactive reauth)", got)
	}
}

func TestDoContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, okSession)
	}))
	defer srv.Close()

	c := testClient(srv)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var rb responseBody
	err := c.do(ctx, http.MethodGet, c.baseURL()+"/x", nil, &rb)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v, want context.Canceled", err)
	}
}

func TestConcurrentDo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{"recordId":"1"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var rb responseBody
			if err := c.do(context.Background(), http.MethodGet, c.baseURL()+"/x", nil, &rb); err != nil {
				t.Errorf("do: %v", err)
			}
			_ = c.LastActivity()
		}()
	}
	wg.Wait()
}

func TestDestroy(t *testing.T) {
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
	if err := c.Destroy(context.Background()); err != nil {
		t.Fatalf("Destroy: %v", err)
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
