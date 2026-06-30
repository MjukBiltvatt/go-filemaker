package filemaker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestRunScript(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotMethod, gotPath, gotQuery = r.Method, r.URL.Path, r.URL.RawQuery
		mu.Unlock()
		writeJSON(w, `{"response":{"scriptResult":"42","scriptError":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.RunScript(context.Background(), "Sales", "ComputeTotal", "42")
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}

	if !resp.Script.OK() {
		t.Errorf("Script.OK() = false, want true (Script.Error = %q)", resp.Script.Error)
	}
	if resp.Script.Result != "42" {
		t.Errorf("Script.Result = %q, want %q", resp.Script.Result, "42")
	}

	mu.Lock()
	method, path, query := gotMethod, gotPath, gotQuery
	mu.Unlock()
	if method != http.MethodGet {
		t.Errorf("method = %q, want GET", method)
	}
	if !strings.HasSuffix(path, "/layouts/Sales/script/ComputeTotal") {
		t.Errorf("path = %q, want suffix /layouts/Sales/script/ComputeTotal", path)
	}
	if query != "script.param=42" {
		t.Errorf("query = %q, want script.param=42", query)
	}
}

func TestRunScriptNoParam(t *testing.T) {
	var mu sync.Mutex
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotQuery = r.URL.RawQuery
		mu.Unlock()
		writeJSON(w, `{"response":{"scriptError":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.RunScript(context.Background(), "Sales", "ComputeTotal", "")
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}
	if !resp.Script.Ran() {
		t.Error("Script.Ran() = false, want true")
	}

	mu.Lock()
	query := gotQuery
	mu.Unlock()
	if query != "" {
		t.Errorf("query = %q, want empty (no script.param when param is empty)", query)
	}
}

func TestRunScriptEscapesName(t *testing.T) {
	var mu sync.Mutex
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		mu.Unlock()
		writeJSON(w, `{"response":{"scriptError":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	_, err := c.RunScript(context.Background(), "Sales #1", "Compute Total", "")
	if err != nil {
		t.Fatalf("RunScript: %v", err)
	}

	mu.Lock()
	path := gotPath
	mu.Unlock()
	// The server receives the decoded path; the layout and script segments must
	// each be a single path segment (no slash injection) with the decoded name.
	if !strings.HasSuffix(path, "/layouts/Sales #1/script/Compute Total") {
		t.Errorf("path = %q, want suffix /layouts/Sales #1/script/Compute Total", path)
	}
}

func TestRunScriptScriptError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{"scriptResult":"","scriptError":"401"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.RunScript(context.Background(), "Sales", "FailScript", "")
	if err != nil {
		t.Fatalf("RunScript: %v", err) // request succeeded; script error is in the response
	}
	if !resp.Script.Ran() {
		t.Error("Script.Ran() = false, want true")
	}
	if resp.Script.OK() {
		t.Errorf("Script.OK() = true, want false (Error = %q)", resp.Script.Error)
	}
	if resp.Script.Error != "401" {
		t.Errorf("Script.Error = %q, want %q", resp.Script.Error, "401")
	}
}

func TestRunScriptValidation(t *testing.T) {
	c, _ := New("https://example.com", "db", "user", "pass")
	tests := []struct {
		name   string
		layout string
		script string
	}{
		{"no layout", "", "MyScript"},
		{"no script", "MyLayout", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.RunScript(context.Background(), tt.layout, tt.script, "")
			if err == nil {
				t.Error("want error, got nil")
			}
		})
	}
}
