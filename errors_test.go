package filemaker

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
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

func TestHTTPErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		err  *HTTPError
		want string
	}{
		{
			"status with a registered text",
			&HTTPError{StatusCode: 502},
			"filemaker: unexpected response (HTTP 502 Bad Gateway)",
		},
		{
			// Cloudflare's range is unregistered, so net/http has no text for it and
			// the body is all the caller gets.
			"status without a registered text",
			&HTTPError{StatusCode: 522, snippet: "Connection timed out"},
			"filemaker: unexpected response (HTTP 522): Connection timed out",
		},
		{
			// The status is context, not the complaint, so a 200 reads correctly.
			"success status with a foreign body",
			&HTTPError{StatusCode: 200, snippet: `{"status":"ok"}`},
			`filemaker: unexpected response (HTTP 200 OK): {"status":"ok"}`,
		},
		{
			"decode failure leads with what failed",
			&HTTPError{StatusCode: 200, Err: errors.New("invalid character '<'")},
			"filemaker: failed to decode response (HTTP 200 OK): invalid character '<'",
		},
		{
			// Printing both would report the byte that broke the parse and then the
			// same byte at the head of the snippet.
			"snippet supersedes the decode failure",
			&HTTPError{StatusCode: 502, Err: errors.New("invalid character '<'"), snippet: "<html>Error 1016</html>"},
			"filemaker: failed to decode response (HTTP 502 Bad Gateway): <html>Error 1016</html>",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestHTTPErrorUnwrap(t *testing.T) {
	decodeErr := errors.New("boom")
	err := error(&HTTPError{StatusCode: 502, Err: decodeErr})
	if !errors.Is(err, decodeErr) {
		t.Error("errors.Is should reach the wrapped decode failure")
	}

	// A sentinel match must not leak across the two error types: an HTTPError is
	// by definition a response the host never gave a code for.
	if errors.Is(err, ErrInvalidToken) {
		t.Error("HTTPError must not match a host-code sentinel")
	}
	if (&HTTPError{StatusCode: 502}).Unwrap() != nil {
		t.Error("Unwrap should be nil when there was no decode failure")
	}
}

func TestBodySnippet(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"empty", "", ""},
		{"whitespace only", " \n\t ", ""},
		{"collapses newlines and runs of spaces", "<html>\n  <body>502</body>\n</html>", "<html> <body>502</body> </html>"},
		{"short body is kept whole", "Origin DNS error", "Origin DNS error"},
		{"long body is truncated", strings.Repeat("a", 250), strings.Repeat("a", 200) + "…"},
		// Truncating by byte would split the final rune and produce invalid UTF-8.
		{"truncates on a rune boundary", strings.Repeat("ä", 250), strings.Repeat("ä", 200) + "…"},
		// Only the first 4 KiB is examined, and that byte cut can land inside a
		// rune. Padding with spaces keeps the collapsed result under the rune
		// limit, so a partial tail would survive to the output if it were not
		// trimmed. "€" is three bytes: with 4091 spaces the cut at 4096 keeps the
		// first one whole and two bytes of the second.
		{"partial rune at the byte cap is dropped", strings.Repeat(" ", 4091) + strings.Repeat("€", 3), "€"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bodySnippet([]byte(tt.body))
			if got != tt.want {
				t.Errorf("bodySnippet() = %q, want %q", got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("bodySnippet() = %q, want valid UTF-8", got)
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

// TestRecordErrIdentity pins the recordErr rule across every endpoint that
// addresses a record: each failure after the argument checks names the record
// and layout, and still unwraps to what the host or transport reported.
func TestRecordErrIdentity(t *testing.T) {
	const identity = `record "9" in layout "People"`
	ctx := context.Background()
	endpoints := []struct {
		name string
		call func(*Client) error
	}{
		{"GetByID", func(c *Client) error {
			_, err := c.GetByID(ctx, "People", "9")
			return err
		}},
		{"UpdateByID", func(c *Client) error {
			_, err := c.UpdateByID(ctx, "People", "9", FieldData{"Name": "Jane"}, WithModID("3"))
			return err
		}},
		{"DeleteByID", func(c *Client) error {
			_, err := c.DeleteByID(ctx, "People", "9")
			return err
		}},
		{"DuplicateByID", func(c *Client) error {
			_, err := c.DuplicateByID(ctx, "People", "9")
			return err
		}},
		{"UploadToContainerByID", func(c *Client) error {
			return c.UploadToContainerByID(ctx, "People", "9", "Photo", "pic.png", strings.NewReader("x"), WithModID("3"))
		}},
	}
	failures := []struct {
		name  string
		body  string
		check func(*testing.T, error)
	}{
		{"conflict", `{"response":{},"messages":[{"code":"306","message":"Record modification ID does not match"}]}`, func(t *testing.T, err error) {
			if !errors.Is(err, ErrRecordModified) {
				t.Errorf("got %v, want ErrRecordModified", err)
			}
		}},
		{"host", `{"response":{},"messages":[{"code":"101","message":"Record is missing"}]}`, func(t *testing.T, err error) {
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Code() != 101 {
				t.Errorf("got %v, want it to wrap the host's *APIError (101)", err)
			}
			if !strings.Contains(err.Error(), "Record is missing") {
				t.Errorf("err = %q, want the host message", err)
			}
		}},
		{"non-API", `<html>Bad Gateway</html>`, func(t *testing.T, err error) {
			var httpErr *HTTPError
			if !errors.As(err, &httpErr) {
				t.Errorf("got %v, want it to wrap *HTTPError", err)
			}
		}},
	}
	for _, e := range endpoints {
		for _, f := range failures {
			t.Run(e.name+"/"+f.name, func(t *testing.T) {
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					writeJSON(w, f.body)
				}))
				defer srv.Close()

				err := e.call(testClient(srv))
				if err == nil {
					t.Fatal("got nil error")
				}
				if !strings.Contains(err.Error(), identity) {
					t.Errorf("err = %q, want it to name %s", err, identity)
				}
				f.check(t, err)
			})
		}
	}
}

func TestRecordErrNil(t *testing.T) {
	if err := recordErr(nil, "People", "9"); err != nil {
		t.Errorf("recordErr(nil) = %v, want nil", err)
	}
}
