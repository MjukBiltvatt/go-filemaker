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
	c.autoReauth = true
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

	c := testClient(srv) // autoReauth defaults to false
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
