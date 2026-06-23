package filemaker

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
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
	if resp.Records[0].ID != "1" || resp.Records[0].ModID != "3" || resp.Records[0].Layout != "People" {
		t.Errorf("record[0] = %+v", resp.Records[0])
	}
	if resp.Records[0].FieldData["Name"] != "Mark" {
		t.Errorf("Name = %v, want Mark", resp.Records[0].FieldData["Name"])
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

func TestUpdate(t *testing.T) {
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
	resp, err := c.Update(context.Background(), "People", "9", FieldData{"Name": "Jane"})
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

func TestDelete(t *testing.T) {
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
	if err := c.Delete(context.Background(), "People", "9"); err != nil {
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

func TestUploadToContainer(t *testing.T) {
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
	err := c.UploadToContainer(context.Background(), "People", "1", "Photo", "pic.png", strings.NewReader("imgdata"))
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

func TestContainerData(t *testing.T) {
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
	rec := Record{Layout: "People", FieldData: FieldData{"Photo": srv.URL + "/Streaming/abc"}}
	data, err := c.ContainerData(context.Background(), rec, "Photo")
	if err != nil {
		t.Fatalf("ContainerData: %v", err)
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

func TestContainerDataForeignHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("must not request a foreign host")
	}))
	defer srv.Close()

	c := testClient(srv)
	rec := Record{FieldData: FieldData{"Photo": "https://evil.example.com/steal"}}
	if _, err := c.ContainerData(context.Background(), rec, "Photo"); err == nil {
		t.Fatal("expected error for foreign-host container URL")
	}
}
