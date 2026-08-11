package filemaker

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestWithDebugLogsAndRedacts drives a real login through the debug transport and
// checks that both the request and response are dumped, that the Authorization
// header is masked, and that the credentials never leak into the log.
func TestWithDebugLogsAndRedacts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, okSession)
	}))
	defer srv.Close()

	var buf bytes.Buffer
	c, err := New(srv.URL, "db", "user", "pass", WithInsecureHTTP(), WithDebug(&buf))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	out := buf.String()

	if !strings.Contains(out, "filemaker debug: request") {
		t.Error("log missing request dump")
	}
	if !strings.Contains(out, "filemaker debug: response") {
		t.Error("log missing response dump")
	}
	// The request body ({}) and the response body (the token envelope) prove
	// bodies are dumped and, for the request, preserved for the round-trip.
	//
	// The session token is deliberately NOT redacted here: WithDebug's doc
	// comment states that it reaches the log and that the log must be handled as
	// a secret. Redacting it means amending that promise too.
	if !strings.Contains(out, `"token":"tok"`) {
		t.Error("log missing response body")
	}

	if !strings.Contains(out, "Authorization: REDACTED") {
		t.Error("Authorization header was not redacted")
	}
	creds := base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if strings.Contains(out, creds) {
		t.Error("Basic-auth credentials leaked into the debug log")
	}
}

// TestWithDebugPreservesRequestBody confirms the host still receives the request
// body even though the debug transport dumps it first.
func TestWithDebugPreservesRequestBody(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, okSession)
	}))
	defer srv.Close()

	c, err := New(srv.URL, "db", "user", "pass", WithInsecureHTTP(), WithDebug(&bytes.Buffer{}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := c.Authenticate(context.Background()); err != nil {
		t.Fatalf("Authenticate: %v", err)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()
	if body != "{}" {
		t.Errorf("server received body %q, want %q", body, "{}")
	}
}
