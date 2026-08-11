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

func TestSingleRecordQuery(t *testing.T) {
	tests := []struct {
		name string
		p    params
		want map[string]string
	}{
		{
			name: "empty",
			p:    params{},
			want: map[string]string{},
		},
		{
			name: "portals",
			p:    params{portals: []string{"Orders", "Notes"}},
			want: map[string]string{"portal": `["Orders","Notes"]`},
		},
		{
			name: "portal paging uses underscore prefix",
			p: params{
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
			p:    params{responseLayout: "PeopleAPI"},
			want: map[string]string{"layout.response": "PeopleAPI"},
		},
		{
			name: "script",
			p:    params{script: scriptCall{name: "MyScript", param: "p"}},
			want: map[string]string{"script": "MyScript", "script.param": "p"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.p.singleRecordQuery()
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

func TestGetRange(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotMethod, gotPath = r.Method, r.URL.Path
		mu.Unlock()
		writeJSON(w, `{"response":{
			"dataInfo":{"database":"db","layout":"People","table":"People","totalRecordCount":3,"foundCount":3,"returnedCount":2},
			"data":[
				{"recordId":"1","modId":"0","fieldData":{"Name":"Alice"},"portalData":{}},
				{"recordId":"2","modId":"1","fieldData":{"Name":"Bob"},"portalData":{}}
			]
		},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.GetRange(context.Background(), "People")
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}

	if len(resp.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(resp.Records))
	}
	if resp.Records[0].ID() != "1" || resp.Records[0].Layout() != "People" {
		t.Errorf("record[0] = {ID:%q Layout:%q}", resp.Records[0].ID(), resp.Records[0].Layout())
	}
	if resp.Records[0].Get("Name") != "Alice" {
		t.Errorf("Name = %v, want Alice", resp.Records[0].Get("Name"))
	}
	if resp.DataInfo.TotalRecordCount != 3 || resp.DataInfo.ReturnedCount != 2 {
		t.Errorf("dataInfo = %+v", resp.DataInfo)
	}

	mu.Lock()
	method, path := gotMethod, gotPath
	mu.Unlock()
	if method != http.MethodGet {
		t.Errorf("method = %q, want GET", method)
	}
	if !strings.HasSuffix(path, "/layouts/People/records") {
		t.Errorf("path = %q, want suffix /layouts/People/records", path)
	}
}

func TestGetRangeValidation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer srv.Close()
	c := testClient(srv)

	if _, err := c.GetRange(context.Background(), ""); err == nil {
		t.Error("expected error for empty layout")
	}
}

func TestGetRangeEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"401","message":"No records match the request"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.GetRange(context.Background(), "People")
	if err != nil {
		t.Fatalf("GetRange with no records: %v", err)
	}
	if resp.Records == nil {
		t.Error("Records = nil, want empty non-nil slice")
	}
	if len(resp.Records) != 0 {
		t.Errorf("Records = %d, want 0", len(resp.Records))
	}
}

func TestGetRangeWithPagingAndSort(t *testing.T) {
	var mu sync.Mutex
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotRawQuery = r.URL.RawQuery
		mu.Unlock()
		writeJSON(w, `{"response":{"data":[]},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.GetRange(context.Background(), "People",
		WithOffset(5),
		WithLimit(10),
		WithSort(SortRule{Field: "Name", Order: SortAscending}),
	); err != nil {
		t.Fatalf("GetRange: %v", err)
	}

	mu.Lock()
	rawQuery := gotRawQuery
	mu.Unlock()

	vals, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("ParseQuery(%q): %v", rawQuery, err)
	}
	if vals.Get("_offset") != "5" {
		t.Errorf("_offset = %q, want 5", vals.Get("_offset"))
	}
	if vals.Get("_limit") != "10" {
		t.Errorf("_limit = %q, want 10", vals.Get("_limit"))
	}
	if vals.Get("_sort") != `[{"fieldName":"Name","sortOrder":"ascend"}]` {
		t.Errorf("_sort = %q, want JSON-encoded sort array", vals.Get("_sort"))
	}
}

func TestGetRangeWithReadOptions(t *testing.T) {
	var mu sync.Mutex
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotRawQuery = r.URL.RawQuery
		mu.Unlock()
		writeJSON(w, `{"response":{"data":[]},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.GetRange(context.Background(), "People",
		WithPortals("Orders"),
		WithPortalLimit("Orders", 5),
		WithPortalOffset("Orders", 2),
		WithResponseLayout("PeopleAPI"),
	); err != nil {
		t.Fatalf("GetRange: %v", err)
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

func TestGetRangeWithScript(t *testing.T) {
	var mu sync.Mutex
	var gotRawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotRawQuery = r.URL.RawQuery
		mu.Unlock()
		writeJSON(w, `{"response":{
			"data":[],
			"scriptResult":"ok",
			"scriptError":"0"
		},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.GetRange(context.Background(), "People", WithScript("MyScript", "param"))
	if err != nil {
		t.Fatalf("GetRange: %v", err)
	}

	mu.Lock()
	rawQuery := gotRawQuery
	mu.Unlock()

	vals, _ := url.ParseQuery(rawQuery)
	if vals.Get("script") != "MyScript" {
		t.Errorf("script = %q, want MyScript", vals.Get("script"))
	}
	if vals.Get("script.param") != "param" {
		t.Errorf("script.param = %q, want param", vals.Get("script.param"))
	}
	if !resp.Scripts.Script.OK() {
		t.Errorf("Scripts.Script.OK = false; Error = %q", resp.Scripts.Script.Error)
	}
}

func TestRecordRangeQuery(t *testing.T) {
	tests := []struct {
		name string
		p    params
		want map[string]string
	}{
		{
			name: "empty",
			p:    params{},
			want: map[string]string{},
		},
		{
			name: "offset and limit use underscore prefix",
			p:    params{offset: 10, limit: 25},
			want: map[string]string{"_offset": "10", "_limit": "25"},
		},
		{
			name: "sort is JSON-encoded with underscore prefix",
			p: params{
				sort: []SortRule{
					{Field: "Name", Order: SortAscending},
					{Field: "Age", Order: SortDescending},
				},
			},
			want: map[string]string{
				"_sort": `[{"fieldName":"Name","sortOrder":"ascend"},{"fieldName":"Age","sortOrder":"descend"}]`,
			},
		},
		{
			name: "portals",
			p:    params{portals: []string{"Orders"}},
			want: map[string]string{"portal": `["Orders"]`},
		},
		{
			name: "portal paging uses underscore prefix",
			p: params{
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
			p:    params{responseLayout: "API"},
			want: map[string]string{"layout.response": "API"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.p.recordRangeQuery()
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
