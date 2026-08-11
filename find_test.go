package filemaker

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestFind(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{
			"dataInfo":{"database":"db","layout":"People","table":"People","totalRecordCount":10,"foundCount":2,"returnedCount":2},
			"data":[
				{"recordId":"1","modId":"3","fieldData":{"Name":"Mark","Age":42},"portalData":{}},
				{"recordId":"2","modId":"0","fieldData":{"Name":"Jane"},"portalData":{}}
			]
		},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.Find(context.Background(), "People", []FindRequest{{Criteria: map[string]string{"Name": "Mark"}}})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	if len(resp.Records) != 2 {
		t.Fatalf("records = %d, want 2", len(resp.Records))
	}
	if resp.Records[0].ID() != "1" || resp.Records[0].ModID() != "3" || resp.Records[0].Layout() != "People" {
		t.Errorf("record[0] = %+v", resp.Records[0])
	}
	if resp.Records[0].Get("Name") != "Mark" {
		t.Errorf("Name = %v, want Mark", resp.Records[0].Get("Name"))
	}
	if resp.Records[0].Fields()["Name"] != "Mark" {
		t.Errorf("Fields()[Name] = %v, want Mark", resp.Records[0].Fields()["Name"])
	}
	if resp.DataInfo.FoundCount != 2 || resp.DataInfo.TotalRecordCount != 10 {
		t.Errorf("dataInfo = %+v", resp.DataInfo)
	}

	mu.Lock()
	method, path, body := gotMethod, gotPath, gotBody
	mu.Unlock()
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
	if !strings.HasSuffix(path, "/layouts/People/_find") {
		t.Errorf("path = %q", path)
	}
	if !strings.Contains(body, `"query":[{"Name":"Mark"}]`) {
		t.Errorf("body = %q", body)
	}
}

func TestFindWithPortalData(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{"data":[{"recordId":"1","modId":"0","fieldData":{},"portalData":{
			"Orders":[
				{"recordId":"10","Orders::Item":"Widget","Orders::Qty":3},
				{"recordId":"11","Orders::Item":"Gadget","Orders::Qty":1}
			],
			"Notes":[
				{"recordId":"20","Notes::Body":"first note"}
			]
		}}]},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	resp, err := testClient(srv).Find(context.Background(), "People", []FindRequest{{Criteria: map[string]string{"Name": "Mark"}}})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(resp.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(resp.Records))
	}

	portals := resp.Records[0].Portals()

	orders := portals["Orders"]
	if len(orders) != 2 {
		t.Fatalf("Orders rows = %d, want 2", len(orders))
	}
	if got := orders[0]["recordId"]; got != "10" {
		t.Errorf("Orders[0] recordId = %v, want 10", got)
	}
	if got := orders[0]["Orders::Item"]; got != "Widget" {
		t.Errorf("Orders[0] Item = %v, want Widget", got)
	}
	if got := orders[0]["Orders::Qty"]; got != float64(3) {
		t.Errorf("Orders[0] Qty = %v, want 3", got)
	}
	if got := orders[1]["Orders::Item"]; got != "Gadget" {
		t.Errorf("Orders[1] Item = %v, want Gadget", got)
	}

	notes := portals["Notes"]
	if len(notes) != 1 {
		t.Fatalf("Notes rows = %d, want 1", len(notes))
	}
	if got := notes[0]["Notes::Body"]; got != "first note" {
		t.Errorf("Notes[0] Body = %v, want \"first note\"", got)
	}
}

func TestFindWithReadManyOptions(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"data":[]},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.Find(context.Background(), "People",
		[]FindRequest{{Criteria: map[string]string{"Name": "Mark"}}},
		WithSort(SortRule{Field: "Name", Order: SortAscending}),
		WithLimit(10),
		WithOffset(5),
	); err != nil {
		t.Fatalf("Find: %v", err)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()
	want := `{"limit":10,"offset":5,"query":[{"Name":"Mark"}],"sort":[{"fieldName":"Name","sortOrder":"ascend"}]}`
	if body != want {
		t.Errorf("body =\n %s\nwant:\n %s", body, want)
	}
}

func TestFindWithReadOptions(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"data":[]},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.Find(context.Background(), "People",
		[]FindRequest{{Criteria: map[string]string{"Name": "Mark"}}},
		WithResponseLayout("PeopleAPI"),
		WithPortals("Orders", "Notes"),
		WithPortalLimit("Orders", 5),
		WithPortalOffset("Orders", 2),
	); err != nil {
		t.Fatalf("Find: %v", err)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()

	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal body %q: %v", body, err)
	}
	if got["layout.response"] != "PeopleAPI" {
		t.Errorf("layout.response = %v, want PeopleAPI (body %q)", got["layout.response"], body)
	}
	portals, _ := got["portal"].([]any)
	if len(portals) != 2 || portals[0] != "Orders" || portals[1] != "Notes" {
		t.Errorf("portal = %v, want [Orders Notes]", got["portal"])
	}
	// Per-portal paging rides in dynamic "offset.<name>"/"limit.<name>" keys.
	if got["limit.Orders"] != float64(5) || got["offset.Orders"] != float64(2) {
		t.Errorf("portal paging = offset.Orders=%v limit.Orders=%v, want 2/5 (body %q)", got["offset.Orders"], got["limit.Orders"], body)
	}
}

func TestFindWithScript(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"data":[],"scriptResult":"done","scriptError":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.Find(context.Background(), "People",
		[]FindRequest{{Criteria: map[string]string{"Name": "Mark"}}},
		WithScript("AfterFind", "p1"),
	)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}

	// The script directives go in the find body, like create/update.
	mu.Lock()
	body := gotBody
	mu.Unlock()
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal body %q: %v", body, err)
	}
	if got["script"] != "AfterFind" || got["script.param"] != "p1" {
		t.Errorf("script keys = %v, want script=AfterFind script.param=p1 (body %q)", got, body)
	}
	// The outcome is decoded into FindResponse.Scripts.
	if resp.Scripts.Script.Result != "done" || !resp.Scripts.Script.OK() {
		t.Errorf("Scripts.Script = %+v, want {done 0}", resp.Scripts.Script)
	}
}

func TestFindStampsLocation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{"data":[{"recordId":"1","modId":"0","fieldData":{"Created":"01/02/2006 15:04:05"},"portalData":{}}]},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	loc := time.FixedZone("TEST", 2*60*60)
	c := testClient(srv)
	c.location = loc

	resp, err := c.Find(context.Background(), "People", nil)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(resp.Records) != 1 {
		t.Fatalf("records = %d, want 1", len(resp.Records))
	}
	if got := resp.Records[0].Time("Created").Location(); got != loc {
		t.Errorf("record time location = %v, want %v", got, loc)
	}
}

func TestFindNoRecords(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"401","message":"No records match the request"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.Find(context.Background(), "People", nil)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if resp.Records == nil {
		t.Error("Records is nil, want empty non-nil slice")
	}
	if len(resp.Records) != 0 {
		t.Errorf("records = %d, want 0", len(resp.Records))
	}
}

func TestFindNoLayout(t *testing.T) {
	c := &Client{}
	if _, err := c.Find(context.Background(), "", nil); err == nil {
		t.Error("expected error for empty layout")
	}
}

func TestMarshalFindBody(t *testing.T) {
	tests := []struct {
		name     string
		requests []FindRequest
		p        params
		want     string
	}{
		{
			name: "empty query",
			want: `{"query":[]}`,
		},
		{
			name: "single request with criteria",
			requests: []FindRequest{
				{Criteria: map[string]string{
					"Firstname": "Mark",
					"Age":       "*",
				}},
			},
			want: `{"query":[{"Age":"*","Firstname":"Mark"}]}`,
		},
		{
			name: "omit request",
			requests: []FindRequest{
				{Criteria: map[string]string{"Lastname": "==Johnson"}, Omit: true},
			},
			want: `{"query":[{"Lastname":"==Johnson","omit":"true"}]}`,
		},
		{
			name: "multiple requests",
			requests: []FindRequest{
				{Criteria: map[string]string{"Firstname": "Mark"}},
				{Criteria: map[string]string{"Lastname": "Johnson"}, Omit: true},
			},
			want: `{"query":[{"Firstname":"Mark"},{"Lastname":"Johnson","omit":"true"}]}`,
		},
		{
			name: "limit, offset and sort",
			requests: []FindRequest{
				{Criteria: map[string]string{"Firstname": "Mark"}},
			},
			p: params{
				sort: []SortRule{
					{Field: "Lastname", Order: SortAscending},
					{Field: "Age", Order: SortDescending},
				},
				limit:  10,
				offset: 5,
			},
			want: `{"limit":10,"offset":5,"query":[{"Firstname":"Mark"}],"sort":[{"fieldName":"Lastname","sortOrder":"ascend"},{"fieldName":"Age","sortOrder":"descend"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := marshalFindBody(tt.requests, tt.p)
			if err != nil {
				t.Fatalf("marshalFindBody returned error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got:  %s\nwant: %s", got, tt.want)
			}
		})
	}
}
