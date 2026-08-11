package filemaker

import (
	"errors"
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

func TestAPIErrorIsIgnoresSecondaryMessage(t *testing.T) {
	err := error(&APIError{Messages: []Message{{Code: 500, Text: "x"}, {Code: 952, Text: "y"}}})
	if errors.Is(err, ErrInvalidToken) {
		t.Errorf("errors.Is should not match a code carried only by a secondary message")
	}

	// The primary still matches with an unrelated secondary message present.
	err = error(&APIError{Messages: []Message{{Code: 401, Text: "x"}, {Code: 500, Text: "y"}}})
	if !errors.Is(err, ErrNoRecords) {
		t.Errorf("errors.Is should match the primary message regardless of secondaries")
	}
}
