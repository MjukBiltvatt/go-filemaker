package filemaker

import (
	"context"
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
		authSem:    make(chan struct{}, 1),
	}
}

func TestNormalizeHost(t *testing.T) {
	cases := []struct {
		in            string
		allowInsecure bool
		want          string
		wantErr       bool
	}{
		{in: "my.host.com", want: "https://my.host.com"},
		{in: "https://my.host.com", want: "https://my.host.com"},
		{in: "http://localhost:8080", wantErr: true},
		{in: "http://localhost:8080", allowInsecure: true, want: "http://localhost:8080"},
		{in: "HTTP://localhost:8080", wantErr: true},
		{in: "ftp://my.host.com", wantErr: true},
		{in: "ftp://my.host.com", allowInsecure: true, wantErr: true},
	}
	for _, c := range cases {
		got, err := normalizeHost(c.in, c.allowInsecure)
		if c.wantErr {
			if err == nil {
				t.Errorf("normalizeHost(%q, %v) = %q, want error", c.in, c.allowInsecure, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeHost(%q, %v) unexpected error: %v", c.in, c.allowInsecure, err)
			continue
		}
		if got != c.want {
			t.Errorf("normalizeHost(%q, %v) = %q, want %q", c.in, c.allowInsecure, got, c.want)
		}
	}
}

func TestSameOrigin(t *testing.T) {
	const base = "https://fms.example.com"
	cases := []struct {
		raw  string
		want bool
		desc string
	}{
		{raw: "https://fms.example.com/Streaming_SSL/x.pdf?RCType=E", want: true, desc: "container URL on the session host"},
		{raw: "https://fms.example.com:443/Streaming_SSL/x.pdf", want: true, desc: "explicit default port"},
		{raw: "https://FMS.Example.com/Streaming_SSL/x.pdf", want: true, desc: "host case differs (DNS is case-insensitive)"},
		{raw: "https://fms.example.com.attacker.com/steal", want: false, desc: "host extended with a suffix"},
		{raw: "https://fms.example.com@attacker.com/steal", want: false, desc: "userinfo hides the real host"},
		{raw: "https://fms.example.com.attacker.com:8443/steal", want: false, desc: "suffix-extended host on another port"},
		{raw: "https://fms.example.completely-evil.io/steal", want: false, desc: "suffix continues the last label"},
		{raw: "http://fms.example.com/Streaming_SSL/x.pdf", want: false, desc: "scheme downgraded to plaintext"},
		{raw: "https://evil.example.com/steal", want: false, desc: "unrelated host"},
		{raw: "/Streaming_SSL/x.pdf", want: false, desc: "relative URL has no host to compare"},
		{raw: "://not a url", want: false, desc: "unparseable URL"},
	}
	for _, c := range cases {
		if got := sameOrigin(base, c.raw); got != c.want {
			t.Errorf("sameOrigin(%q, %q) = %v, want %v (%s)", base, c.raw, got, c.want, c.desc)
		}
	}
}

func TestNew(t *testing.T) {
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
	if c.token != "" {
		t.Errorf("token = %q, want empty (no eager auth)", c.token)
	}
	if !c.LastActivity().IsZero() {
		t.Errorf("lastActivity = %v, want zero (no activity yet)", c.LastActivity())
	}
	if n := hits.Load(); n != 0 {
		t.Errorf("New made %d HTTP calls, want 0 (construction is pure)", n)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New("", "db", "u", "p"); err == nil {
		t.Error("expected error for empty host")
	}
	// database and username are no longer validated at construction: a
	// credential-free client is allowed so it can reach ProductInfo. They are
	// instead enforced when a session is established (see TestLoginValidation).
	if _, err := New("h", "", "", ""); err != nil {
		t.Errorf("New with empty database/username: %v, want nil", err)
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
		leaderDone <- c.authenticate(context.Background(), "tok")
	}()
	<-authStarted

	// Follower with an already-cancelled ctx must bail at its deadline rather
	// than wait for the blocked leader.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.authenticate(ctx, "tok"); !errors.Is(err, context.Canceled) {
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

func TestWithDateFormatWiring(t *testing.T) {
	c, err := New("https://example.com", "db", "user", "pass", WithDateFormat(DateFormatISO))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if c.dateFormat == nil || *c.dateFormat != DateFormatISO {
		t.Errorf("dateFormat = %v, want DateFormatISO", c.dateFormat)
	}

	// Unset (nil) by default.
	d, err := New("https://example.com", "db", "user", "pass")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if d.dateFormat != nil {
		t.Errorf("default dateFormat = %v, want nil (unset)", d.dateFormat)
	}
}

// TestWithDateFormatRejectsUnsupported guards that New refuses a format the
// client cannot write, rather than telling the host one format ("dateformats"
// parameter) while writing another (the wrappers fall back to US).
func TestWithDateFormatRejectsUnsupported(t *testing.T) {
	for _, format := range []DateFormat{1, 3, -1} {
		c, err := New("https://example.com", "db", "user", "pass", WithDateFormat(format))
		if err == nil {
			t.Errorf("New with DateFormat(%d) = nil error, want error", format)
		}
		if c != nil {
			t.Errorf("New with DateFormat(%d) returned a client, want nil", format)
		}
	}
}

// TestURLBuildersEscapeSegments guards that the request-path builders percent-
// escape the database, layout, id, and field segments, so names with
// URL-reserved characters (spaces, '#', '/') address the right resource instead
// of corrupting the path. It is the hermetic counterpart to
// TestIntegrationSpecialLayoutNames.
func TestURLBuildersEscapeSegments(t *testing.T) {
	c := &Client{host: "https://h", database: "My DB"}
	const base = "https://h/fmi/data/v1/databases/My%20DB"

	cases := []struct {
		name string
		got  string
		want string
	}{
		{"baseURL", c.baseURL(), base},
		{"layoutURL", c.layoutURL("Sales #1"), base + "/layouts/Sales%20%231"},
		{"findURL", c.findURL("Sales #1"), base + "/layouts/Sales%20%231/_find"},
		{"recordsURL", c.recordsURL("Sales #1"), base + "/layouts/Sales%20%231/records"},
		{"recordURL", c.recordURL("A/B", "7"), base + "/layouts/A%2FB/records/7"},
		{"containerURL", c.containerURL("Lay #2", "7", "My Field"), base + "/layouts/Lay%20%232/records/7/containers/My%20Field"},
		{"globalsURL", c.globalsURL(), base + "/globals"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// With reauth disabled, a 952 from the host should surface as an error that
// callers can branch on with errors.Is rather than inspecting numeric codes.
func TestInvalidTokenSentinelThroughDo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"952","message":"Invalid FileMaker Data API token"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv) // reauthOnInvalidToken defaults to false
	var rb responseBody
	err := c.do(context.Background(), http.MethodGet, c.baseURL()+"/x", nil, &rb)
	if !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("got %v, want errors.Is ErrInvalidToken", err)
	}
}
