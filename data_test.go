package filemaker

import (
	"context"
	"encoding/json"
	"errors"
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
	resp, err := c.Find(context.Background(), "People", Query{
		Requests: []Request{{Criteria: map[string]string{"Name": "Mark"}}},
	})
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

func TestFindStampsLocation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{"data":[{"recordId":"1","modId":"0","fieldData":{"Created":"01/02/2006 15:04:05"},"portalData":{}}]},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	loc := time.FixedZone("TEST", 2*60*60)
	c := testClient(srv)
	c.location = loc

	resp, err := c.Find(context.Background(), "People", Query{})
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
	resp, err := c.Find(context.Background(), "People", Query{})
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
	if _, err := c.Find(context.Background(), "", Query{}); err == nil {
		t.Error("expected error for empty layout")
	}
}

func TestCreate(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"recordId":"7","modId":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.Create(context.Background(), "People", FieldData{"Name": "Mark"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.RecordID != "7" || resp.ModID != "0" {
		t.Errorf("resp = %+v", resp)
	}

	mu.Lock()
	method, path, body := gotMethod, gotPath, gotBody
	mu.Unlock()
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
	if !strings.HasSuffix(path, "/layouts/People/records") {
		t.Errorf("path = %q", path)
	}
	if !strings.Contains(body, `"fieldData":{"Name":"Mark"}`) {
		t.Errorf("body = %q", body)
	}
}

func TestCreateNilFields(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"recordId":"1","modId":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.Create(context.Background(), "People", nil); err != nil {
		t.Fatalf("Create: %v", err)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()
	if !strings.Contains(body, `"fieldData":{}`) {
		t.Errorf("body = %q, want empty fieldData object", body)
	}
}

func TestUpdateByID(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"modId":"5"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.UpdateByID(context.Background(), "People", "9", FieldData{"Name": "Jane"})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if resp.ModID != "5" {
		t.Errorf("modID = %q, want 5", resp.ModID)
	}

	mu.Lock()
	method, path, body := gotMethod, gotPath, gotBody
	mu.Unlock()
	if method != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", method)
	}
	if !strings.HasSuffix(path, "/layouts/People/records/9") {
		t.Errorf("path = %q", path)
	}
	if !strings.Contains(body, `"fieldData":{"Name":"Jane"}`) {
		t.Errorf("body = %q", body)
	}
}

func TestUpdateWithModID(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"modId":"4"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.UpdateByID(context.Background(), "People", "9", FieldData{"Name": "Jane"}, WithModID("3"))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if resp.ModID != "4" {
		t.Errorf("modID = %q, want 4", resp.ModID)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()
	if !strings.Contains(body, `"modId":"3"`) {
		t.Errorf("body = %q, want it to contain the modId", body)
	}
	if !strings.Contains(body, `"fieldData":{"Name":"Jane"}`) {
		t.Errorf("body = %q, want fieldData", body)
	}
}

func TestUpdateModIDMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"306","message":"Record modification ID does not match"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	_, err := c.UpdateByID(context.Background(), "People", "9", FieldData{"Name": "Jane"}, WithModID("3"))
	if !errors.Is(err, ErrRecordModified) {
		t.Fatalf("got %v, want ErrRecordModified", err)
	}
}

func TestUpdateByRecord(t *testing.T) {
	var mu sync.Mutex
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		mu.Unlock()
		writeJSON(w, `{"response":{"modId":"5"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	rec := Record{layout: "People", id: "9"}
	if _, err := c.Update(context.Background(), rec, FieldData{"Name": "Jane"}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	mu.Lock()
	path := gotPath
	mu.Unlock()
	if !strings.HasSuffix(path, "/layouts/People/records/9") {
		t.Errorf("path = %q, want it addressed from the record", path)
	}
}

func TestRecordWriteNoID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not make a request for a record without an ID")
	}))
	defer srv.Close()

	c := testClient(srv)
	rec := Record{layout: "People"} // no ID

	if _, err := c.Update(context.Background(), rec, FieldData{"Name": "x"}); err == nil {
		t.Error("Update: expected error for record without ID")
	}
	if err := c.Delete(context.Background(), rec); err == nil {
		t.Error("Delete: expected error for record without ID")
	}
	if err := c.UploadToContainer(context.Background(), rec, "Photo", "f.png", strings.NewReader("x")); err == nil {
		t.Error("UploadToContainer: expected error for record without ID")
	}
}

func TestUpdateIfUnchanged(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"modId":"4"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	rec := Record{layout: "People", id: "9", modID: "3"}

	lastBody := func() string {
		mu.Lock()
		defer mu.Unlock()
		return gotBody
	}

	// IfUnchanged locks against the record's own ModID.
	if _, err := c.Update(context.Background(), rec, FieldData{"Name": "Jane"}, IfUnchanged()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !strings.Contains(lastBody(), `"modId":"3"`) {
		t.Errorf("body = %q, want modId 3 from the record", lastBody())
	}

	// Combining the two is order-independent: WithModID pins the explicit version.
	if _, err := c.Update(context.Background(), rec, FieldData{"Name": "Jane"}, IfUnchanged(), WithModID("9")); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !strings.Contains(lastBody(), `"modId":"9"`) {
		t.Errorf("body = %q, want modId 9 (WithModID pins the version)", lastBody())
	}

	// Same result with the options in the opposite order.
	if _, err := c.Update(context.Background(), rec, FieldData{"Name": "Jane"}, WithModID("9"), IfUnchanged()); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !strings.Contains(lastBody(), `"modId":"9"`) {
		t.Errorf("body = %q, want modId 9 (order-independent)", lastBody())
	}
}

func TestWithModIDEmpty(t *testing.T) {
	var mu sync.Mutex
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		writeJSON(w, `{"response":{"modId":"4"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	rec := Record{layout: "People", id: "9", modID: "3"}

	if _, err := c.Update(context.Background(), rec, FieldData{"Name": "x"}, WithModID("")); err == nil {
		t.Error("Update: expected error for WithModID(\"\")")
	}
	if _, err := c.UpdateByID(context.Background(), "People", "9", FieldData{"Name": "x"}, WithModID("")); err == nil {
		t.Error("UpdateByID: expected error for WithModID(\"\")")
	}

	mu.Lock()
	n := hits
	mu.Unlock()
	if n != 0 {
		t.Errorf("made %d HTTP calls, want 0 (WithModID(\"\") errors before the request)", n)
	}
}

func TestIfUnchangedErrors(t *testing.T) {
	var mu sync.Mutex
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		writeJSON(w, `{"response":{"modId":"4"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)

	// IfUnchanged on a record without a ModID must error, not silently degrade to
	// an unconditional write.
	if _, err := c.Update(context.Background(), Record{layout: "People", id: "9"}, FieldData{"Name": "x"}, IfUnchanged()); err == nil {
		t.Error("Update: expected error for IfUnchanged on a record without a ModID")
	}

	// IfUnchanged on the id-addressed form has no record to source a ModID from.
	if _, err := c.UpdateByID(context.Background(), "People", "9", FieldData{"Name": "x"}, IfUnchanged()); err == nil {
		t.Error("UpdateByID: expected error for IfUnchanged without a record")
	}

	mu.Lock()
	n := hits
	mu.Unlock()
	if n != 0 {
		t.Errorf("made %d HTTP calls, want 0 (both should error before the request)", n)
	}
}

func TestUpdateDoesNotMutateCallerOpts(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{"modId":"4"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	rec := Record{layout: "People", id: "9", modID: "3"}

	// A slice with spare capacity (len 1, cap 2) whose extra slot holds a sentinel.
	// If Update appends its resolved WithModID into the caller's array instead of a
	// fresh one, it clobbers the sentinel at index 1.
	var sentinelCalled bool
	sentinel := UpdateOption(func(cfg *updateConfig) { sentinelCalled = true })
	backing := []UpdateOption{IfUnchanged(), sentinel}
	opts := backing[:1] // len 1, cap 2, shares backing with sentinel at [1]

	if _, err := c.Update(context.Background(), rec, FieldData{"Name": "x"}, opts...); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// backing[1] must still be the sentinel — invoke it and confirm it runs.
	var cfg updateConfig
	backing[1](&cfg)
	if !sentinelCalled {
		t.Error("Update mutated the caller's opts backing array (sentinel at index 1 was overwritten)")
	}
}

func TestUpdateWithPortalData(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"modId":"5"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	portals := PortalData{
		"Orders": {
			// An existing related record to edit. The record ID and mod ID are
			// plain keys, not table-occurrence qualified like the field values:
			// the host reads "Orders::recordId" as a field and rejects the edit
			// (code 102). Field values stay qualified ("Orders::Qty").
			{"recordId": "70", "modId": "4", "Orders::Qty": 3},
			// A new related record to add (no recordId).
			{"Orders::Item": "Widget"},
		},
	}
	if _, err := c.UpdateByID(context.Background(), "People", "9", FieldData{"Name": "Jane"}, WithPortalData(portals)); err != nil {
		t.Fatalf("UpdateByID: %v", err)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()

	var got struct {
		FieldData  map[string]any              `json:"fieldData"`
		PortalData map[string][]map[string]any `json:"portalData"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal body %q: %v", body, err)
	}
	if got.FieldData["Name"] != "Jane" {
		t.Errorf("fieldData = %v, want Name=Jane", got.FieldData)
	}
	rows := got.PortalData["Orders"]
	if len(rows) != 2 {
		t.Fatalf("portalData[Orders] = %d rows, want 2 (body %q)", len(rows), body)
	}
	if rows[0]["recordId"] != "70" || rows[0]["modId"] != "4" {
		t.Errorf("edit row = %v, want recordId 70 / modId 4", rows[0])
	}
	if _, ok := rows[1]["recordId"]; ok {
		t.Errorf("add row should carry no recordId, got %v", rows[1])
	}
	if rows[1]["Orders::Item"] != "Widget" {
		t.Errorf("add row = %v, want Item=Widget", rows[1])
	}
}

// A plain update with no WithPortalData must not emit a portalData key, and a
// portal-only edit (nil FieldData) must still send an empty fieldData object so
// the host applies the portal edits without touching the record's own fields.
func TestUpdatePortalDataOmittedAndPortalOnly(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"modId":"5"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	lastBody := func() string {
		mu.Lock()
		defer mu.Unlock()
		return gotBody
	}

	if _, err := c.UpdateByID(context.Background(), "People", "9", FieldData{"Name": "Jane"}); err != nil {
		t.Fatalf("UpdateByID: %v", err)
	}
	if strings.Contains(lastBody(), "portalData") {
		t.Errorf("body = %q, want no portalData key when WithPortalData is not used", lastBody())
	}

	portals := PortalData{"Orders": {{"Orders::Item": "Widget"}}}
	if _, err := c.UpdateByID(context.Background(), "People", "9", nil, WithPortalData(portals)); err != nil {
		t.Fatalf("UpdateByID portal-only: %v", err)
	}
	if !strings.Contains(lastBody(), `"fieldData":{}`) {
		t.Errorf("body = %q, want empty fieldData on a portal-only edit", lastBody())
	}
	if !strings.Contains(lastBody(), `"portalData":{"Orders":`) {
		t.Errorf("body = %q, want portalData", lastBody())
	}
}

// The record-based Update threads WithPortalData through to the request, and it
// composes with IfUnchanged so the body carries both the portal edits and the
// record's locking modId.
func TestUpdateByRecordPortalDataWithIfUnchanged(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"modId":"5"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	rec := Record{layout: "People", id: "9", modID: "3"}
	portals := PortalData{"Orders": {{"Orders::Item": "Widget"}}}
	if _, err := c.Update(context.Background(), rec, FieldData{"Name": "Jane"}, WithPortalData(portals), IfUnchanged()); err != nil {
		t.Fatalf("Update: %v", err)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()
	if !strings.Contains(body, `"portalData":{"Orders":`) {
		t.Errorf("body = %q, want portalData threaded through Update", body)
	}
	if !strings.Contains(body, `"modId":"3"`) {
		t.Errorf("body = %q, want IfUnchanged's modId 3", body)
	}
}

func TestDeleteByID(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotMethod, gotPath = r.Method, r.URL.Path
		mu.Unlock()
		writeJSON(w, `{"response":{},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if err := c.DeleteByID(context.Background(), "People", "9"); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	mu.Lock()
	method, path := gotMethod, gotPath
	mu.Unlock()
	if method != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", method)
	}
	if !strings.HasSuffix(path, "/layouts/People/records/9") {
		t.Errorf("path = %q", path)
	}
}

func TestUploadToContainerByID(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotMethod, gotPath, gotContentType, gotBody = r.Method, r.URL.Path, r.Header.Get("Content-Type"), string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	err := c.UploadToContainerByID(context.Background(), "People", "1", "Photo", "pic.png", strings.NewReader("imgdata"))
	if err != nil {
		t.Fatalf("UploadToContainer: %v", err)
	}

	mu.Lock()
	method, path, ct, body := gotMethod, gotPath, gotContentType, gotBody
	mu.Unlock()
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
	if !strings.HasSuffix(path, "/records/1/containers/Photo") {
		t.Errorf("path = %q", path)
	}
	if !strings.HasPrefix(ct, "multipart/form-data") {
		t.Errorf("content-type = %q, want multipart/form-data", ct)
	}
	if !strings.Contains(body, `filename="pic.png"`) || !strings.Contains(body, "imgdata") {
		t.Errorf("body missing filename or data: %q", body)
	}
}

func TestDownloadFromContainer(t *testing.T) {
	var mu sync.Mutex
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		w.Write([]byte("filecontents"))
	}))
	defer srv.Close()

	c := testClient(srv)
	rec := Record{layout: "People", fieldData: map[string]any{"Photo": srv.URL + "/Streaming/abc"}}
	data, err := c.DownloadFromContainer(context.Background(), rec, "Photo")
	if err != nil {
		t.Fatalf("DownloadFromContainer: %v", err)
	}
	if string(data) != "filecontents" {
		t.Errorf("data = %q, want filecontents", data)
	}

	mu.Lock()
	auth := gotAuth
	mu.Unlock()
	if auth != "Bearer tok" {
		t.Errorf("auth = %q, want Bearer tok", auth)
	}
}

func TestDownloadFromContainerNotAURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not make a request when the field holds no container URL")
	}))
	defer srv.Close()

	c := testClient(srv)
	rec := Record{layout: "People", fieldData: map[string]any{"Age": float64(42)}}
	if _, err := c.DownloadFromContainer(context.Background(), rec, "Age"); err == nil {
		t.Error("expected error for a non-string field")
	}
	if _, err := c.DownloadFromContainer(context.Background(), rec, "Missing"); err == nil {
		t.Error("expected error for a missing field")
	}
}

func TestDownloadFromContainerByURLForeignHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not request a foreign host")
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.DownloadFromContainerByURL(context.Background(), "https://evil.example.com/steal"); err == nil {
		t.Fatal("expected error for foreign-host container URL")
	}
}

func TestDownloadFromContainerByURLEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not make a request for an empty container URL")
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.DownloadFromContainerByURL(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty container URL")
	}
}

func TestCreateWithDateFormatISO(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"recordId":"1","modId":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	iso := DateFormatISO
	c.dateFormat = &iso
	if _, err := c.Create(context.Background(), "People", FieldData{
		"DOB":     Date(time.Date(1990, 6, 23, 0, 0, 0, 0, time.UTC)),
		"Created": Timestamp(time.Date(2026, 6, 23, 14, 5, 0, 0, time.UTC)),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()
	if !strings.Contains(body, `"dateformats":2`) {
		t.Errorf("body = %q, want dateformats=2", body)
	}
	if !strings.Contains(body, `"DOB":"1990-06-23"`) || !strings.Contains(body, `"Created":"2026-06-23 14:05:00"`) {
		t.Errorf("body = %q, want ISO-formatted date and timestamp", body)
	}
}

func TestCreateWithoutDateFormatOmitsParam(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"recordId":"1","modId":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	// testClient leaves dateFormat at its zero value (unset).
	c := testClient(srv)
	if _, err := c.Create(context.Background(), "People", FieldData{
		"DOB": Date(time.Date(1990, 6, 23, 0, 0, 0, 0, time.UTC)),
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()
	if strings.Contains(body, "dateformats") {
		t.Errorf("body = %q, want no dateformats parameter when unset", body)
	}
	if !strings.Contains(body, `"DOB":"06/23/1990"`) {
		t.Errorf("body = %q, want US DOB when unset", body)
	}
}
