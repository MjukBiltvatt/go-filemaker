package filemaker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAPIErrorIs(t *testing.T) {
	tests := []struct {
		name     string
		code     int
		sentinel error
		want     bool
	}{
		{"no records matches 401", 401, ErrNoRecords, true},
		{"invalid token matches 952", 952, ErrInvalidToken, true},
		{"record modified matches 306", 306, ErrRecordModified, true},
		{"mismatched code", 401, ErrInvalidToken, false},
		{"unmapped sentinel", 952, ErrNotNumber, false},
		{"unrelated code", 500, ErrInvalidToken, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := error(&APIError{Messages: []Message{{Code: tt.code, Text: "x"}}})
			if got := errors.Is(err, tt.sentinel); got != tt.want {
				t.Errorf("errors.Is(APIError{%d}, %v) = %v, want %v", tt.code, tt.sentinel, got, tt.want)
			}
		})
	}
}

func TestAPIErrorIsMatchesAnyMessage(t *testing.T) {
	err := error(&APIError{Messages: []Message{{Code: 500, Text: "x"}, {Code: 952, Text: "y"}}})
	if !errors.Is(err, ErrInvalidToken) {
		t.Errorf("errors.Is should match a non-primary message carrying the code")
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
