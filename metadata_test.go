package filemaker

import (
	"context"
	"net/http"
	"net/http/httptest"
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
