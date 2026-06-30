package filemaker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func TestGetByID(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotMethod, gotPath = r.Method, r.URL.Path
		mu.Unlock()
		writeJSON(w, `{"response":{
			"data":[
				{"recordId":"42","modId":"3","fieldData":{"Name":"Mark","Age":41.0},"portalData":{}}
			]
		},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.GetByID(context.Background(), "People", "42")
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	if resp.Record.ID() != "42" || resp.Record.ModID() != "3" || resp.Record.Layout() != "People" {
		t.Errorf("record = {ID:%q ModID:%q Layout:%q}", resp.Record.ID(), resp.Record.ModID(), resp.Record.Layout())
	}
	if resp.Record.Get("Name") != "Mark" {
		t.Errorf("Name = %v, want Mark", resp.Record.Get("Name"))
	}

	mu.Lock()
	method, path := gotMethod, gotPath
	mu.Unlock()
	if method != http.MethodGet {
		t.Errorf("method = %q, want GET", method)
	}
	if !strings.HasSuffix(path, "/layouts/People/records/42") {
		t.Errorf("path = %q, want suffix /layouts/People/records/42", path)
	}
}

func TestGet(t *testing.T) {
	var mu sync.Mutex
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		mu.Unlock()
		writeJSON(w, `{"response":{
			"data":[{"recordId":"7","modId":"1","fieldData":{"Name":"Jane"},"portalData":{}}]
		},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	rec := Record{id: "7", layout: "People"}
	resp, err := c.Get(context.Background(), rec)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if resp.Record.ID() != "7" {
		t.Errorf("ID = %q, want 7", resp.Record.ID())
	}

	mu.Lock()
	path := gotPath
	mu.Unlock()
	if !strings.HasSuffix(path, "/layouts/People/records/7") {
		t.Errorf("path = %q, want suffix /layouts/People/records/7", path)
	}
}

func TestGetValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := testClient(srv)

	if _, err := c.GetByID(context.Background(), "", "42"); err == nil {
		t.Error("expected error for empty layout")
	}
	if _, err := c.GetByID(context.Background(), "People", ""); err == nil {
		t.Error("expected error for empty id")
	}
	if _, err := c.Get(context.Background(), Record{}); err == nil {
		t.Error("expected error for record with no ID")
	}
}

func TestGetWithReadOptions(t *testing.T) {
	var mu sync.Mutex
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotRawQuery = r.URL.RawQuery
		mu.Unlock()
		writeJSON(w, `{"response":{
			"data":[{"recordId":"1","modId":"0","fieldData":{},"portalData":{}}]
		},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.GetByID(context.Background(), "People", "1",
		WithPortals("Orders"),
		WithPortalLimit("Orders", 5),
		WithPortalOffset("Orders", 2),
		WithResponseLayout("PeopleAPI"),
	); err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	mu.Lock()
	rawQuery := gotRawQuery
	mu.Unlock()

	vals, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", rawQuery, err)
	}
	if vals.Get("portal") != `["Orders"]` {
		t.Errorf("portal = %q, want %q", vals.Get("portal"), `["Orders"]`)
	}
	if vals.Get("_limit.Orders") != "5" {
		t.Errorf("_limit.Orders = %q, want 5", vals.Get("_limit.Orders"))
	}
	if vals.Get("_offset.Orders") != "2" {
		t.Errorf("_offset.Orders = %q, want 2", vals.Get("_offset.Orders"))
	}
	if vals.Get("layout.response") != "PeopleAPI" {
		t.Errorf("layout.response = %q, want PeopleAPI", vals.Get("layout.response"))
	}
}

func TestGetWithScript(t *testing.T) {
	var mu sync.Mutex
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotRawQuery = r.URL.RawQuery
		mu.Unlock()
		writeJSON(w, `{"response":{
			"data":[{"recordId":"1","modId":"0","fieldData":{},"portalData":{}}],
			"scriptResult":"echo-result",
			"scriptError":"0"
		},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.GetByID(context.Background(), "People", "1", WithScript("EchoParam", "hello"))
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	mu.Lock()
	rawQuery := gotRawQuery
	mu.Unlock()

	vals, _ := url.ParseQuery(rawQuery)
	if vals.Get("script") != "EchoParam" {
		t.Errorf("script = %q, want EchoParam", vals.Get("script"))
	}
	if vals.Get("script.param") != "hello" {
		t.Errorf("script.param = %q, want hello", vals.Get("script.param"))
	}
	if !resp.Scripts.Script.OK() {
		t.Errorf("Scripts.Script.OK = false; Error = %q", resp.Scripts.Script.Error)
	}
	if resp.Scripts.Script.Result != "echo-result" {
		t.Errorf("Scripts.Script.Result = %q, want echo-result", resp.Scripts.Script.Result)
	}
}

func TestGetQueryParams(t *testing.T) {
	tests := []struct {
		name string
		cfg  recordConfig
		want map[string]string
	}{
		{
			name: "empty",
			cfg:  recordConfig{},
			want: map[string]string{},
		},
		{
			name: "portals",
			cfg:  recordConfig{portals: []string{"Orders", "Notes"}},
			want: map[string]string{"portal": `["Orders","Notes"]`},
		},
		{
			name: "portal paging uses underscore prefix",
			cfg: recordConfig{
				portals:      []string{"Orders"},
				portalRanges: map[string]portalRange{"Orders": {offset: 3, limit: 10}},
			},
			want: map[string]string{
				"portal":         `["Orders"]`,
				"_offset.Orders": "3",
				"_limit.Orders":  "10",
			},
		},
		{
			name: "response layout",
			cfg:  recordConfig{responseLayout: "PeopleAPI"},
			want: map[string]string{"layout.response": "PeopleAPI"},
		},
		{
			name: "script",
			cfg:  recordConfig{script: scriptCall{name: "MyScript", param: "p"}},
			want: map[string]string{"script": "MyScript", "script.param": "p"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.cfg.getQueryParams()
			for key, wantVal := range tt.want {
				if gotVal := got.Get(key); gotVal != wantVal {
					t.Errorf("key %q = %q, want %q", key, gotVal, wantVal)
				}
			}
			if len(got) != len(tt.want) {
				t.Errorf("got %d keys, want %d: %v", len(got), len(tt.want), got)
			}
		})
	}
}
