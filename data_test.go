package filemaker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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

func TestCreateWithPortalData(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"recordId":"7","modId":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	portals := PortalData{"Orders": {{"Orders::Item": "Widget"}}}
	if _, err := c.Create(context.Background(), "People", FieldData{"Name": "Mark"}, WithPortalData(portals)); err != nil {
		t.Fatalf("Create: %v", err)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()
	if !strings.Contains(body, `"fieldData":{"Name":"Mark"}`) {
		t.Errorf("body = %q, want fieldData", body)
	}
	if !strings.Contains(body, `"portalData":{"Orders":`) {
		t.Errorf("body = %q, want portalData threaded through Create", body)
	}
}

func TestCreateWithScript(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"recordId":"7","modId":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.Create(context.Background(), "People", FieldData{"Name": "Mark"},
		WithScript("AfterCreate", "p1"),
		WithPrerequestScript("Pre", ""),
	); err != nil {
		t.Fatalf("Create: %v", err)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()

	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal body %q: %v", body, err)
	}
	if got["script"] != "AfterCreate" || got["script.param"] != "p1" {
		t.Errorf("script keys = %v, want script=AfterCreate script.param=p1 (body %q)", got, body)
	}
	if got["script.prerequest"] != "Pre" {
		t.Errorf("script.prerequest = %v, want Pre", got["script.prerequest"])
	}
	// An empty param is omitted, not sent as an empty string.
	if _, ok := got["script.prerequest.param"]; ok {
		t.Errorf("script.prerequest.param should be omitted for an empty param (body %q)", body)
	}
}

func TestCreateWithDataEntryOptions(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"recordId":"7","modId":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.Create(context.Background(), "People", FieldData{"Name": "Mark"},
		WithEntryMode(EntryModeScript),
		WithProhibitMode(EntryModeUser),
	); err != nil {
		t.Fatalf("Create: %v", err)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()

	var got struct {
		Options map[string]string `json:"options"`
	}
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal body %q: %v", body, err)
	}
	if got.Options["entrymode"] != "script" || got.Options["prohibitmode"] != "user" {
		t.Errorf("options = %v, want entrymode=script prohibitmode=user (body %q)", got.Options, body)
	}
}

func TestDeleteWithScript(t *testing.T) {
	var mu sync.Mutex
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotQuery = r.URL.Query()
		mu.Unlock()
		writeJSON(w, `{"response":{},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.DeleteByID(context.Background(), "People", "9",
		WithScript("AfterDelete", "p1"),
		WithPresortScript("Sorter", "p2"),
	); err != nil {
		t.Fatalf("DeleteByID: %v", err)
	}

	mu.Lock()
	q := gotQuery
	mu.Unlock()
	// Delete has no body, so scripts ride in the query string.
	if q.Get("script") != "AfterDelete" || q.Get("script.param") != "p1" {
		t.Errorf("query = %v, want script=AfterDelete script.param=p1", q)
	}
	if q.Get("script.presort") != "Sorter" || q.Get("script.presort.param") != "p2" {
		t.Errorf("query = %v, want script.presort=Sorter script.presort.param=p2", q)
	}
}

func TestScriptResultsDecoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{
			"recordId":"7","modId":"0",
			"scriptResult":"after-val","scriptError":"0",
			"scriptResult.prerequest":"pre-val","scriptError.prerequest":"3",
			"scriptResult.presort":"sort-val","scriptError.presort":"0"
		},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.Create(context.Background(), "People", FieldData{"Name": "Mark"}, WithScript("S", "p"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	want := ScriptOutcomes{
		Script:     ScriptOutcome{Result: "after-val", Error: "0"},
		Prerequest: ScriptOutcome{Result: "pre-val", Error: "3"},
		Presort:    ScriptOutcome{Result: "sort-val", Error: "0"},
	}
	if resp.Scripts != want {
		t.Errorf("Scripts = %+v, want %+v", resp.Scripts, want)
	}
	if !resp.Scripts.Script.Ran() || !resp.Scripts.Script.OK() {
		t.Errorf("after-script: Ran=%v OK=%v, want true/true", resp.Scripts.Script.Ran(), resp.Scripts.Script.OK())
	}
	// prerequest ran but errored (code 3): Ran true, OK false.
	if !resp.Scripts.Prerequest.Ran() || resp.Scripts.Prerequest.OK() {
		t.Errorf("prerequest: Ran=%v OK=%v, want true/false", resp.Scripts.Prerequest.Ran(), resp.Scripts.Prerequest.OK())
	}
}

// A response with no script keys (no script was requested) must report that no
// script ran — distinguishable from a script that ran and returned an empty
// value, which still carries Error "0".
func TestScriptResultsAbsentWhenNoScript(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{"recordId":"7","modId":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.Create(context.Background(), "People", FieldData{"Name": "Mark"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if resp.Scripts != (ScriptOutcomes{}) {
		t.Errorf("Scripts = %+v, want zero value when no script ran", resp.Scripts)
	}
	if resp.Scripts.Script.Ran() {
		t.Error("Script.Ran() = true, want false when no script ran")
	}
}

func TestDeleteScriptResultsDecoded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{"scriptResult":"done","scriptError":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.DeleteByID(context.Background(), "People", "9", WithScript("S", "p"))
	if err != nil {
		t.Fatalf("DeleteByID: %v", err)
	}
	if resp.Scripts.Script.Result != "done" || resp.Scripts.Script.Error != "0" {
		t.Errorf("Scripts.Script = %+v, want {done 0}", resp.Scripts.Script)
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
	if _, err := c.Delete(context.Background(), rec); err == nil {
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
	var sentinel UpdateOption = option(func(p *params) { sentinelCalled = true })
	backing := []UpdateOption{IfUnchanged(), sentinel}
	opts := backing[:1] // len 1, cap 2, shares backing with sentinel at [1]

	if _, err := c.Update(context.Background(), rec, FieldData{"Name": "x"}, opts...); err != nil {
		t.Fatalf("Update: %v", err)
	}

	// backing[1] must still be the sentinel — invoke it and confirm it runs.
	var p params
	backing[1].applyUpdate(&p)
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
	if _, err := c.DeleteByID(context.Background(), "People", "9"); err != nil {
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

func TestUploadToContainerWithModID(t *testing.T) {
	var mu sync.Mutex
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotQuery = r.URL.Query()
		mu.Unlock()
		writeJSON(w, `{"response":{},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	// Explicit mod ID via the id-addressed form; the container endpoint has no
	// body, so it rides in the query string.
	if err := c.UploadToContainerByID(context.Background(), "People", "1", "Photo", "pic.png", strings.NewReader("x"), WithModID("7")); err != nil {
		t.Fatalf("UploadToContainerByID: %v", err)
	}

	mu.Lock()
	q := gotQuery
	mu.Unlock()
	if q.Get("modId") != "7" {
		t.Errorf("query = %v, want modId=7", q)
	}
}

func TestUploadToContainerIfUnchanged(t *testing.T) {
	var mu sync.Mutex
	var gotQuery url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotQuery = r.URL.Query()
		mu.Unlock()
		writeJSON(w, `{"response":{},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	// IfUnchanged locks against the record's own ModID, sourced here from rec.
	rec := Record{layout: "People", id: "1", modID: "5"}
	if err := c.UploadToContainer(context.Background(), rec, "Photo", "pic.png", strings.NewReader("x"), IfUnchanged()); err != nil {
		t.Fatalf("UploadToContainer: %v", err)
	}

	mu.Lock()
	q := gotQuery
	mu.Unlock()
	if q.Get("modId") != "5" {
		t.Errorf("query = %v, want modId=5 from the record", q)
	}
}

func TestUploadIfUnchangedErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not make a request when IfUnchanged cannot resolve a mod ID")
	}))
	defer srv.Close()

	c := testClient(srv)
	// IfUnchanged on the id-addressed form has no record to source a ModID from.
	if err := c.UploadToContainerByID(context.Background(), "People", "1", "Photo", "f.png", strings.NewReader("x"), IfUnchanged()); err == nil {
		t.Error("UploadToContainerByID: expected error for IfUnchanged without a record")
	}
	// IfUnchanged on a record without a ModID must error, not silently degrade.
	if err := c.UploadToContainer(context.Background(), Record{layout: "People", id: "1"}, "Photo", "f.png", strings.NewReader("x"), IfUnchanged()); err == nil {
		t.Error("UploadToContainer: expected error for IfUnchanged on a record without a ModID")
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

func TestDownloadFromContainerRefreshesIdleSession(t *testing.T) {
	var sessionCalls, downloadCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sessions") {
			sessionCalls.Add(1)
			writeJSON(w, `{"response":{"token":"newtok"},"messages":[{"code":"0","message":"OK"}]}`)
			return
		}
		downloadCalls.Add(1)
		if got := r.Header.Get("Authorization"); got != "Bearer newtok" {
			t.Errorf("download auth = %q, want Bearer newtok (token should be refreshed before send)", got)
		}
		w.Write([]byte("filecontents"))
	}))
	defer srv.Close()

	c := testClient(srv)
	c.idleTimeout = time.Minute
	c.lastActivity = time.Now().Add(-2 * time.Minute) // idle past the threshold

	data, err := c.DownloadFromContainerByURL(context.Background(), srv.URL+"/Streaming/abc")
	if err != nil {
		t.Fatalf("DownloadFromContainerByURL: %v", err)
	}
	if string(data) != "filecontents" {
		t.Errorf("data = %q, want filecontents", data)
	}
	if got := sessionCalls.Load(); got != 1 {
		t.Errorf("session (reauth) calls = %d, want 1 (proactive refresh)", got)
	}
	if got := downloadCalls.Load(); got != 1 {
		t.Errorf("download calls = %d, want 1", got)
	}
}

// A 401 from the streaming endpoint is ambiguous — expired token, aged-out
// container URL, or no access to the field — so it must surface as itself rather
// than be reported as an invalid token and retried on a fresh session.
func TestDownloadFromContainerUnauthorizedIsNotReauthed(t *testing.T) {
	var downloadCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/sessions") {
			t.Error("must not re-authenticate on a container 401")
			writeJSON(w, okSession)
			return
		}
		downloadCalls.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := testClient(srv)
	c.reauthOnInvalidToken = true

	_, err := c.DownloadFromContainerByURL(context.Background(), srv.URL+"/Streaming/abc")
	if err == nil {
		t.Fatal("expected an error for a 401 container response")
	}
	if errors.Is(err, ErrInvalidToken) {
		t.Errorf("err = %v, want no ErrInvalidToken match (401 does not imply 952)", err)
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("err = %v, want the HTTP status reported as-is", err)
	}
	if got := downloadCalls.Load(); got != 1 {
		t.Errorf("download calls = %d, want 1 (no retry)", got)
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

	// Each URL is crafted to look like the session host to a naive prefix
	// match. The bearer token must never reach any of them, so the refusal has
	// to come from the host check — an incidental dial or auth failure would
	// leave the token exposed on a host that did resolve.
	cases := []struct {
		raw  string
		desc string
	}{
		{raw: "https://evil.example.com/steal", desc: "unrelated host"},
		{raw: srv.URL + "@attacker.example/steal", desc: "userinfo hides the real host"},
		{raw: srv.URL + "0/steal", desc: "port extended by a digit"},
		{raw: srv.URL + ".attacker.example/steal", desc: "host extended with a suffix"},
	}
	c := testClient(srv)
	for _, tc := range cases {
		_, err := c.DownloadFromContainerByURL(context.Background(), tc.raw)
		if err == nil {
			t.Errorf("DownloadFromContainerByURL(%q): expected error (%s)", tc.raw, tc.desc)
			continue
		}
		if !strings.Contains(err.Error(), "foreign host") {
			t.Errorf("DownloadFromContainerByURL(%q) = %v, want a foreign-host refusal (%s)", tc.raw, err, tc.desc)
		}
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

func TestDuplicateByID(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"recordId":"12","modId":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.DuplicateByID(context.Background(), "People", "9")
	if err != nil {
		t.Fatalf("DuplicateByID: %v", err)
	}
	if resp.RecordID != "12" || resp.ModID != "0" {
		t.Errorf("resp = %+v", resp)
	}

	mu.Lock()
	method, path, body := gotMethod, gotPath, gotBody
	mu.Unlock()
	if method != http.MethodPost {
		t.Errorf("method = %q, want POST", method)
	}
	if !strings.HasSuffix(path, "/layouts/People/records/9") {
		t.Errorf("path = %q", path)
	}
	if body != `{}` {
		t.Errorf("body = %q, want {} for a script-less duplicate", body)
	}
}

func TestDuplicateByRecord(t *testing.T) {
	var mu sync.Mutex
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotPath = r.URL.Path
		mu.Unlock()
		writeJSON(w, `{"response":{"recordId":"13","modId":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	rec := Record{layout: "People", id: "9"}
	if _, err := c.Duplicate(context.Background(), rec); err != nil {
		t.Fatalf("Duplicate: %v", err)
	}

	mu.Lock()
	path := gotPath
	mu.Unlock()
	if !strings.HasSuffix(path, "/layouts/People/records/9") {
		t.Errorf("path = %q, want it addressed from the record", path)
	}
}

func TestDuplicateWithScript(t *testing.T) {
	var mu sync.Mutex
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotBody = string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{"recordId":"14","modId":"0","scriptResult":"done","scriptError":"0"},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	resp, err := c.DuplicateByID(context.Background(), "People", "9", WithScript("AfterDuplicate", "p1"))
	if err != nil {
		t.Fatalf("DuplicateByID: %v", err)
	}

	mu.Lock()
	body := gotBody
	mu.Unlock()
	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal body %q: %v", body, err)
	}
	if got["script"] != "AfterDuplicate" || got["script.param"] != "p1" {
		t.Errorf("script keys = %v, want script=AfterDuplicate script.param=p1 (body %q)", got, body)
	}
	if _, ok := got["fieldData"]; ok {
		t.Errorf("body = %q, want no fieldData key in a duplicate body", body)
	}
	if resp.Scripts.Script.Result != "done" || !resp.Scripts.Script.OK() {
		t.Errorf("Scripts.Script = %+v, want {done 0}", resp.Scripts.Script)
	}
}

func TestDuplicateNoID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not make a request for a record without an ID")
	}))
	defer srv.Close()

	c := testClient(srv)
	rec := Record{layout: "People"} // no ID
	if _, err := c.Duplicate(context.Background(), rec); err == nil {
		t.Error("Duplicate: expected error for record without ID")
	}
}

func TestDuplicateNoLayout(t *testing.T) {
	c := &Client{}
	if _, err := c.DuplicateByID(context.Background(), "", "9"); err == nil {
		t.Error("DuplicateByID: expected error for empty layout")
	}
}

func TestDuplicateNoRecordID(t *testing.T) {
	c := &Client{}
	if _, err := c.DuplicateByID(context.Background(), "People", ""); err == nil {
		t.Error("DuplicateByID: expected error for empty record id")
	}
}

func TestSetGlobalFields(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(b)
		mu.Unlock()
		writeJSON(w, `{"response":{},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	err := c.SetGlobalFields(context.Background(), FieldData{
		"Contacts::gCompany": "FileMaker",
		"Contacts::gCode":    "95054",
	})
	if err != nil {
		t.Fatalf("SetGlobalFields: %v", err)
	}

	mu.Lock()
	method, path, body := gotMethod, gotPath, gotBody
	mu.Unlock()
	if method != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", method)
	}
	if !strings.HasSuffix(path, "/globals") {
		t.Errorf("path = %q, want suffix /globals", path)
	}

	var got map[string]any
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("unmarshal body %q: %v", body, err)
	}
	gf, ok := got["globalFields"].(map[string]any)
	if !ok {
		t.Fatalf("globalFields missing or wrong type in body %q", body)
	}
	if gf["Contacts::gCompany"] != "FileMaker" {
		t.Errorf("gCompany = %v, want FileMaker", gf["Contacts::gCompany"])
	}
	if gf["Contacts::gCode"] != "95054" {
		t.Errorf("gCode = %v, want 95054", gf["Contacts::gCode"])
	}
}

func TestSetGlobalFieldsNilBecomesEmptyObject(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		writeJSON(w, `{"response":{},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if err := c.SetGlobalFields(context.Background(), nil); err != nil {
		t.Fatalf("SetGlobalFields(nil): %v", err)
	}
	if gotBody != `{"globalFields":{}}` {
		t.Errorf("body = %q, want {\"globalFields\":{}}", gotBody)
	}
}

// TestMarshalRecordBodyDateFormat checks that the body carries a dateformats
// parameter and reformats Date/Timestamp values only when a format is set, and
// is unchanged (no parameter, US values) when unset.
func TestMarshalRecordBodyDateFormat(t *testing.T) {
	fields := FieldData{
		"DOB":     Date(time.Date(1990, 6, 23, 0, 0, 0, 0, time.UTC)),
		"Created": Timestamp(time.Date(2026, 6, 23, 14, 5, 0, 0, time.UTC)),
	}
	us, iso := DateFormatUS, DateFormatISO
	cases := []struct {
		name   string
		format *DateFormat
		want   string
	}{
		{"unset", nil, `{"fieldData":{"Created":"06/23/2026 14:05:00","DOB":"06/23/1990"}}`},
		{"US", &us, `{"dateformats":0,"fieldData":{"Created":"06/23/2026 14:05:00","DOB":"06/23/1990"}}`},
		{"ISO", &iso, `{"dateformats":2,"fieldData":{"Created":"2026-06-23 14:05:00","DOB":"1990-06-23"}}`},
	}
	for _, c := range cases {
		body, err := marshalRecordBody(fields, params{}, c.format)
		if err != nil {
			t.Fatalf("%s: marshalRecordBody: %v", c.name, err)
		}
		if string(body) != c.want {
			t.Errorf("%s:\n got: %s\nwant: %s", c.name, body, c.want)
		}
	}
}

func TestFieldDataWithTypedValues(t *testing.T) {
	body, err := marshalRecordBody(FieldData{
		"Active":  Bool(true),
		"DOB":     Date(time.Date(1990, 6, 23, 0, 0, 0, 0, time.UTC)),
		"Created": Timestamp(time.Date(2026, 6, 23, 14, 5, 0, 0, time.UTC)),
		"Name":    "Mark",
	}, params{}, nil)
	if err != nil {
		t.Fatalf("marshalRecordBody: %v", err)
	}
	want := `{"fieldData":{"Active":1,"Created":"06/23/2026 14:05:00","DOB":"06/23/1990","Name":"Mark"}}`
	if string(body) != want {
		t.Errorf("got:  %s\nwant: %s", body, want)
	}
}
