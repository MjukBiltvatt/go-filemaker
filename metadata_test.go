package filemaker

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
)

func TestProductInfo(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		mu.Unlock()
		writeJSON(w, `{"response":{"productInfo":{
			"name":"FileMaker Data API Engine",
			"dateFormat":"MM/dd/yyyy",
			"timeFormat":"HH:mm:ss",
			"timeStampFormat":"MM/dd/yyyy HH:mm:ss"
		}},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	info, err := c.ProductInfo(context.Background())
	if err != nil {
		t.Fatalf("ProductInfo: %v", err)
	}

	want := ProductInfo{
		Name:            "FileMaker Data API Engine",
		DateFormat:      "MM/dd/yyyy",
		TimeFormat:      "HH:mm:ss",
		TimeStampFormat: "MM/dd/yyyy HH:mm:ss",
	}
	if info != want {
		t.Errorf("info = %+v, want %+v", info, want)
	}

	mu.Lock()
	method, path, auth := gotMethod, gotPath, gotAuth
	mu.Unlock()
	if method != http.MethodGet {
		t.Errorf("method = %q, want GET", method)
	}
	if path != "/fmi/data/v1/productInfo" {
		t.Errorf("path = %q, want /fmi/data/v1/productInfo", path)
	}
	// The endpoint is unauthenticated and not database-scoped: no bearer token
	// is sent and the path carries no /databases/ segment.
	if auth != "" {
		t.Errorf("Authorization = %q, want empty (unauthenticated)", auth)
	}
}

func TestProductInfoWithoutCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{"productInfo":{"name":"FileMaker Data API Engine","dateFormat":"MM/dd/yyyy"}},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	// A client built with neither database nor username can still reach the
	// unauthenticated ProductInfo endpoint.
	c, err := New(srv.URL, "", "", "", WithInsecureHTTP())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	info, err := c.ProductInfo(context.Background())
	if err != nil {
		t.Fatalf("ProductInfo: %v", err)
	}
	if info.Name != "FileMaker Data API Engine" || info.DateFormat != "MM/dd/yyyy" {
		t.Errorf("info = %+v", info)
	}
}

func TestProductInfoAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"1200","message":"Generic calculation error"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.ProductInfo(context.Background()); err == nil {
		t.Error("expected error from non-zero message code")
	}
}

func TestProductInfoDoesNotStampActivity(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{"productInfo":{"name":"FileMaker Data API Engine"}},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.ProductInfo(context.Background()); err != nil {
		t.Fatalf("ProductInfo: %v", err)
	}
	if !c.LastActivity().IsZero() {
		t.Errorf("LastActivity = %v, want zero (metadata call is not session activity)", c.LastActivity())
	}
}

func TestDatabases(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		mu.Unlock()
		writeJSON(w, `{"response":{"databases":[{"name":"Customers"},{"name":"Sales"}]},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	dbs, err := c.Databases(context.Background())
	if err != nil {
		t.Fatalf("Databases: %v", err)
	}

	if len(dbs) != 2 || dbs[0].Name != "Customers" || dbs[1].Name != "Sales" {
		t.Errorf("databases = %+v", dbs)
	}

	mu.Lock()
	method, path, auth := gotMethod, gotPath, gotAuth
	mu.Unlock()
	if method != http.MethodGet {
		t.Errorf("method = %q, want GET", method)
	}
	if path != "/fmi/data/v1/databases" {
		t.Errorf("path = %q, want /fmi/data/v1/databases", path)
	}
	// The endpoint uses Basic auth with the raw credentials, not the session
	// bearer token (testClient holds a token "tok", which must not be sent).
	wantAuth := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:pass"))
	if auth != wantAuth {
		t.Errorf("auth = %q, want %q (Basic, not Bearer)", auth, wantAuth)
	}
	// Host-level metadata call: it is not session activity.
	if !c.LastActivity().IsZero() {
		t.Errorf("LastActivity = %v, want zero", c.LastActivity())
	}
}

func TestDatabasesWithoutCredentials(t *testing.T) {
	var mu sync.Mutex
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotAuth = r.Header.Get("Authorization")
		mu.Unlock()
		writeJSON(w, `{"response":{"databases":[{"name":"Customers"}]},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	// A credential-free client sends no Authorization header, which works against
	// a host that has "Filter Databases in Client Applications" disabled.
	c, err := New(srv.URL, "", "", "", WithInsecureHTTP())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	dbs, err := c.Databases(context.Background())
	if err != nil {
		t.Fatalf("Databases: %v", err)
	}
	if len(dbs) != 1 || dbs[0].Name != "Customers" {
		t.Errorf("databases = %+v", dbs)
	}

	mu.Lock()
	auth := gotAuth
	mu.Unlock()
	if auth != "" {
		t.Errorf("Authorization = %q, want none (no credentials configured)", auth)
	}
}

func TestDatabasesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"802","message":"Unable to open file"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.Databases(context.Background()); err == nil {
		t.Error("expected error from non-zero message code")
	}
}

func TestScripts(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		mu.Unlock()
		writeJSON(w, `{"response":{"scripts":[
			{"name":"Daily Cleanup","isFolder":false},
			{"name":"Reports","isFolder":true,"folderScriptNames":[
				{"name":"Monthly","isFolder":false},
				{"name":"Archived","isFolder":true,"folderScriptNames":[
					{"name":"Legacy","isFolder":false}
				]}
			]}
		]},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	scripts, err := c.Scripts(context.Background())
	if err != nil {
		t.Fatalf("Scripts: %v", err)
	}

	want := []Script{
		{Name: "Daily Cleanup", IsFolder: false},
		{Name: "Reports", IsFolder: true, FolderScriptNames: []Script{
			{Name: "Monthly", IsFolder: false},
			{Name: "Archived", IsFolder: true, FolderScriptNames: []Script{
				{Name: "Legacy", IsFolder: false},
			}},
		}},
	}
	if !reflect.DeepEqual(scripts, want) {
		t.Errorf("scripts = %+v, want %+v", scripts, want)
	}

	mu.Lock()
	method, path, auth := gotMethod, gotPath, gotAuth
	mu.Unlock()
	if method != http.MethodGet {
		t.Errorf("method = %q, want GET", method)
	}
	// The endpoint is database-scoped: the path carries the /databases/db/ segment.
	if path != "/fmi/data/v1/databases/db/scripts" {
		t.Errorf("path = %q, want /fmi/data/v1/databases/db/scripts", path)
	}
	// Unlike the host-level metadata calls, this one authenticates with the
	// session bearer token (testClient holds token "tok").
	if auth != "Bearer tok" {
		t.Errorf("auth = %q, want %q (Bearer session token)", auth, "Bearer tok")
	}
	// It is a database-scoped session call, so it counts as session activity.
	if c.LastActivity().IsZero() {
		t.Error("LastActivity is zero, want stamped (scripts is a session call)")
	}
}

func TestScriptsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"802","message":"Unable to open file"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.Scripts(context.Background()); err == nil {
		t.Error("expected error from non-zero message code")
	}
}

func TestLayouts(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("Authorization")
		mu.Unlock()
		writeJSON(w, `{"response":{"layouts":[
			{"name":"Customers"},
			{"name":"Details"},
			{"name":"Package Management","isFolder":true,"folderLayoutNames":[
				{"name":"Mark as sent"},
				{"name":"Subfolder","isFolder":true,"folderLayoutNames":[
					{"name":"Find Unsent"}
				]}
			]}
		]},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	layouts, err := c.Layouts(context.Background())
	if err != nil {
		t.Fatalf("Layouts: %v", err)
	}

	want := []Layout{
		{Name: "Customers"},
		{Name: "Details"},
		{Name: "Package Management", IsFolder: true, FolderLayoutNames: []Layout{
			{Name: "Mark as sent"},
			{Name: "Subfolder", IsFolder: true, FolderLayoutNames: []Layout{
				{Name: "Find Unsent"},
			}},
		}},
	}
	if !reflect.DeepEqual(layouts, want) {
		t.Errorf("layouts = %+v, want %+v", layouts, want)
	}

	mu.Lock()
	method, path, auth := gotMethod, gotPath, gotAuth
	mu.Unlock()
	if method != http.MethodGet {
		t.Errorf("method = %q, want GET", method)
	}
	// The endpoint is database-scoped: the path carries the /databases/db/ segment.
	if path != "/fmi/data/v1/databases/db/layouts" {
		t.Errorf("path = %q, want /fmi/data/v1/databases/db/layouts", path)
	}
	// Like Scripts, it authenticates with the session bearer token, not Basic.
	if auth != "Bearer tok" {
		t.Errorf("auth = %q, want %q (Bearer session token)", auth, "Bearer tok")
	}
	// It is a database-scoped session call, so it counts as session activity.
	if c.LastActivity().IsZero() {
		t.Error("LastActivity is zero, want stamped (layouts is a session call)")
	}
}

func TestLayoutsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"802","message":"Unable to open file"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.Layouts(context.Background()); err == nil {
		t.Error("expected error from non-zero message code")
	}
}

func TestLayoutMetadata(t *testing.T) {
	var mu sync.Mutex
	var gotMethod, gotPath, gotQuery, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotMethod, gotPath, gotQuery, gotAuth = r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		mu.Unlock()
		writeJSON(w, `{"response":{
			"fieldMetaData":[
				{"name":"TextField","type":"normal","displayType":"editText","result":"text","global":false,"maxRepeat":1,"notEmpty":false},
				{"name":"Status","type":"normal","displayType":"popupList","result":"text","valueList":"Statuses","maxRepeat":1,"maxCharacters":20,"notEmpty":true}
			],
			"portalMetaData":{
				"ChildTable":[
					{"name":"ChildTable::ChildText","type":"normal","displayType":"editText","result":"text","maxRepeat":1}
				]
			},
			"valueLists":[
				{"name":"Statuses","type":"customList","values":[
					{"value":"open","displayValue":"Open"},
					{"value":"closed","displayValue":"Closed"}
				]}
			]
		},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	meta, err := c.LayoutMetadata(context.Background(), "Package Management")
	if err != nil {
		t.Fatalf("LayoutMetadata: %v", err)
	}

	want := LayoutMetadata{
		FieldMetadata: []FieldMetadata{
			{Name: "TextField", Type: "normal", DisplayType: "editText", Result: "text", MaxRepeat: 1},
			{Name: "Status", Type: "normal", DisplayType: "popupList", Result: "text", ValueList: "Statuses", MaxRepeat: 1, MaxCharacters: 20, NotEmpty: true},
		},
		PortalMetadata: map[string][]FieldMetadata{
			"ChildTable": {
				{Name: "ChildTable::ChildText", Type: "normal", DisplayType: "editText", Result: "text", MaxRepeat: 1},
			},
		},
		ValueLists: []ValueList{
			{Name: "Statuses", Type: "customList", Values: []ValueListItem{
				{Value: "open", DisplayValue: "Open"},
				{Value: "closed", DisplayValue: "Closed"},
			}},
		},
	}
	if !reflect.DeepEqual(meta, want) {
		t.Errorf("meta = %+v, want %+v", meta, want)
	}

	mu.Lock()
	method, path, query, auth := gotMethod, gotPath, gotQuery, gotAuth
	mu.Unlock()
	if method != http.MethodGet {
		t.Errorf("method = %q, want GET", method)
	}
	// The layout name is database-scoped and URL-escaped into the path; no
	// recordId option means no query string.
	if path != "/fmi/data/v1/databases/db/layouts/Package Management" {
		t.Errorf("path = %q, want /fmi/data/v1/databases/db/layouts/Package Management", path)
	}
	if query != "" {
		t.Errorf("query = %q, want empty (no recordId option)", query)
	}
	if auth != "Bearer tok" {
		t.Errorf("auth = %q, want %q (Bearer session token)", auth, "Bearer tok")
	}
	if c.LastActivity().IsZero() {
		t.Error("LastActivity is zero, want stamped (layout metadata is a session call)")
	}
}

func TestLayoutMetadataWithValueListRecordID(t *testing.T) {
	var mu sync.Mutex
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		gotQuery = r.URL.RawQuery
		mu.Unlock()
		writeJSON(w, `{"response":{"fieldMetaData":[],"valueLists":[]},"messages":[{"code":"0","message":"OK"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.LayoutMetadata(context.Background(), "ParentTable", WithValueListRecordID("123")); err != nil {
		t.Fatalf("LayoutMetadata: %v", err)
	}

	mu.Lock()
	query := gotQuery
	mu.Unlock()
	if query != "recordId=123" {
		t.Errorf("query = %q, want recordId=123", query)
	}
}

func TestLayoutMetadataAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"response":{},"messages":[{"code":"105","message":"Layout is missing"}]}`)
	}))
	defer srv.Close()

	c := testClient(srv)
	if _, err := c.LayoutMetadata(context.Background(), "Nonexistent"); err == nil {
		t.Error("expected error from non-zero message code")
	}
}
