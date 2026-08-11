package filemaker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNilOptionsAreSkipped covers the nil guard in option resolution: a nil option
// in the variadic slice must be skipped rather than dereferenced, and an option
// after it must still apply. Every endpoint that takes options is exercised, since
// each resolves its own option interface.
func TestNilOptionsAreSkipped(t *testing.T) {
	// One payload serving every endpoint: the record fields satisfy the writes and
	// the data array satisfies the reads. Unknown keys are ignored per endpoint.
	const body = `{"response":{"recordId":"7","modId":"0","data":[{"fieldData":{},"portalData":{},"recordId":"7","modId":"0"}]},"messages":[{"code":"0","message":"OK"}]}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, body)
	}))
	defer srv.Close()

	c := testClient(srv)
	ctx := context.Background()
	rec := Record{layout: "People", id: "7", modID: "3"}
	fields := FieldData{"Name": "x"}

	cases := []struct {
		name string
		call func() error
	}{
		{"Create", func() error {
			_, err := c.Create(ctx, "People", fields, nil, WithScript("S", ""))
			return err
		}},
		{"Update", func() error {
			_, err := c.Update(ctx, rec, fields, nil, IfUnchanged())
			return err
		}},
		{"UpdateByID", func() error {
			_, err := c.UpdateByID(ctx, "People", "7", fields, nil, WithScript("S", ""))
			return err
		}},
		{"Delete", func() error {
			_, err := c.Delete(ctx, rec, nil, WithScript("S", ""))
			return err
		}},
		{"DeleteByID", func() error {
			_, err := c.DeleteByID(ctx, "People", "7", nil, WithScript("S", ""))
			return err
		}},
		{"Duplicate", func() error {
			_, err := c.Duplicate(ctx, rec, nil, WithScript("S", ""))
			return err
		}},
		{"DuplicateByID", func() error {
			_, err := c.DuplicateByID(ctx, "People", "7", nil, WithScript("S", ""))
			return err
		}},
		{"Find", func() error {
			_, err := c.Find(ctx, "People", []FindRequest{{Criteria: Criteria{"Name": "x"}}}, nil, WithLimit(5))
			return err
		}},
		{"Get", func() error {
			_, err := c.Get(ctx, rec, nil, WithPortals("Orders"))
			return err
		}},
		{"GetByID", func() error {
			_, err := c.GetByID(ctx, "People", "7", nil, WithPortals("Orders"))
			return err
		}},
		{"GetRange", func() error {
			_, err := c.GetRange(ctx, "People", nil, WithLimit(5))
			return err
		}},
		{"UploadToContainer", func() error {
			return c.UploadToContainer(ctx, rec, "File", "a.txt", strings.NewReader("x"), nil, WithModID("3"))
		}},
		{"UploadToContainerByID", func() error {
			return c.UploadToContainerByID(ctx, "People", "7", "File", "a.txt", strings.NewReader("x"), nil, WithModID("3"))
		}},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(); err != nil {
				t.Fatalf("%s with a nil option: %v", tt.name, err)
			}
		})
	}
}

// TestDeferredOptionErrorBeatsUnresolvableModID pins the error precedence when an
// option's deferred validation error and an unresolvable IfUnchanged both apply:
// the option error wins, since it names the actual mistake. WithModID("") sets the
// deferred error and marks the write conditional, so the *ByID form (no record to
// source a mod ID from) would otherwise report the IfUnchanged hint instead. The
// two options are order-independent, so both orders are checked.
func TestDeferredOptionErrorBeatsUnresolvableModID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("made an HTTP call; expected the option error to surface first")
	}))
	defer srv.Close()

	c := testClient(srv)
	ctx := context.Background()
	const want = "filemaker: WithModID requires a non-empty mod ID"

	orders := []struct {
		name string
		opts []UpdateOption
	}{
		{"WithModID first", []UpdateOption{WithModID(""), IfUnchanged()}},
		{"IfUnchanged first", []UpdateOption{IfUnchanged(), WithModID("")}},
	}
	for _, tt := range orders {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.UpdateByID(ctx, "People", "9", FieldData{"Name": "x"}, tt.opts...)
			if err == nil {
				t.Fatal("expected an error")
			}
			if err.Error() != want {
				t.Errorf("error = %q, want %q", err.Error(), want)
			}
		})
	}
}
