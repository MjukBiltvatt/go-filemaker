package filemaker

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func testRecord() Record {
	return Record{
		layout: "People",
		fieldData: map[string]any{
			"string":              "string",
			"int":                 float64(100),
			"int8":                float64(8),
			"int16":               float64(16),
			"int32":               float64(32),
			"int64":               float64(64),
			"float32":             float64(32.32),
			"float64":             float64(64.64),
			"bool_true_txt_test":  "test",
			"bool_true_txt_false": "false",
			"bool_false_txt":      "",
			"bool_true_num_1":     float64(1),
			"bool_true_num_123":   float64(123),
			"bool_false_num_0":    float64(0),
			"date_1":              "01/02/2006",
			"date_2":              "2006-01-02",
			"timestamp_1":         "01/02/2006 15:04:05",
			"timestamp_2":         "2006-01-02 15:04:05",
			"time_invalid":        "january 1 2006 15 pm",
			"list_lf":             "a\nb\nc",
			"list_crlf":           "a\r\nb\r\nc\r\n",
			"list_cr":             "a\rb\rc",
			"list_single":         "only",
			"list_blank_internal": "a\n\nb",
			"list_empty":          "",
		},
	}
}

func TestFieldsAndPortalsAreFaithfulCopies(t *testing.T) {
	cases := []struct {
		name string
		rec  Record
	}{
		{"nil", Record{}},
		{"empty", Record{fieldData: map[string]any{}, portalData: map[string][]map[string]any{}}},
		{"populated", Record{
			fieldData: map[string]any{"Name": "Mark", "Age": float64(42), "Note": ""},
			portalData: map[string][]map[string]any{
				"Lines":   {{"Item": "Widget", "Qty": float64(3)}, {"Item": "Gadget", "Qty": float64(0)}},
				"Empty":   {},
				"NilRows": nil,
			},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rec.Fields(); !reflect.DeepEqual(got, FieldData(tc.rec.fieldData)) {
				t.Errorf("Fields() = %#v, want %#v", got, tc.rec.fieldData)
			}
			if got := tc.rec.Portals(); !reflect.DeepEqual(got, PortalData(tc.rec.portalData)) {
				t.Errorf("Portals() = %#v, want %#v", got, tc.rec.portalData)
			}
		})
	}

	// Mutating the returned copies must not reach the record.
	rec := Record{
		fieldData:  map[string]any{"Name": "Mark"},
		portalData: map[string][]map[string]any{"Lines": {{"Item": "Widget"}}},
	}
	f := rec.Fields()
	f["Name"] = "CHANGED"
	f["New"] = "x"
	p := rec.Portals()
	p["Lines"][0]["Item"] = "CHANGED"
	p["Lines"] = append(p["Lines"], map[string]any{"Item": "Extra"})

	if rec.Get("Name") != "Mark" {
		t.Errorf("record Name mutated to %v via Fields() copy", rec.Get("Name"))
	}
	if rec.Has("New") {
		t.Error("record gained a field via mutated Fields() copy")
	}
	if got := rec.portalData["Lines"][0]["Item"]; got != "Widget" {
		t.Errorf("portal row mutated to %v via Portals() copy", got)
	}
	if n := len(rec.portalData["Lines"]); n != 1 {
		t.Errorf("portal slice grew to %d rows via Portals() copy", n)
	}
}

func TestRecordHas(t *testing.T) {
	r := testRecord()
	if !r.Has("string") {
		t.Error("Has(string) = false, want true")
	}
	if r.Has("missing") {
		t.Error("Has(missing) = true, want false")
	}
	if !r.Has("bool_false_txt") {
		t.Error("Has(bool_false_txt) = false, want true (present but empty)")
	}
}

func TestRecordGetters(t *testing.T) {
	r := testRecord()

	t.Run("Get", func(t *testing.T) {
		if r.Get("string") != "string" {
			t.Errorf("Get = %v", r.Get("string"))
		}
		if r.Get("missing") != nil {
			t.Errorf("Get missing = %v, want nil", r.Get("missing"))
		}
	})

	t.Run("String", func(t *testing.T) {
		if got := r.String("string"); got != "string" {
			t.Errorf("String = %q", got)
		}
		if _, err := r.StringE("int"); !errors.Is(err, ErrNotString) {
			t.Errorf("StringE(int) err = %v, want ErrNotString", err)
		}
	})

	t.Run("StringSlice", func(t *testing.T) {
		// CR, LF and CRLF are all accepted as line breaks, and a single
		// trailing line break (list_crlf) is trimmed rather than yielding "".
		want := []string{"a", "b", "c"}
		for _, field := range []string{"list_lf", "list_crlf", "list_cr"} {
			got := r.StringSlice(field)
			if !slices.Equal(got, want) {
				t.Errorf("StringSlice(%q) = %#v, want %#v", field, got, want)
			}
		}
		if got := r.StringSlice("list_single"); !slices.Equal(got, []string{"only"}) {
			t.Errorf("StringSlice(single) = %#v", got)
		}
		// Blank lines between values are preserved.
		if got := r.StringSlice("list_blank_internal"); !slices.Equal(got, []string{"a", "", "b"}) {
			t.Errorf("StringSlice(blank_internal) = %#v, want [a  b]", got)
		}
		// An empty field yields nil, not []string{""}.
		if got := r.StringSlice("list_empty"); got != nil {
			t.Errorf("StringSlice(empty) = %#v, want nil", got)
		}
		// A missing field is not a string, so it also yields nil.
		if got := r.StringSlice("missing"); got != nil {
			t.Errorf("StringSlice(missing) = %#v, want nil", got)
		}
		if _, err := r.StringSliceE("int"); !errors.Is(err, ErrNotString) {
			t.Errorf("StringSliceE(int) err = %v, want ErrNotString", err)
		}
	})

	t.Run("ints", func(t *testing.T) {
		if got := r.Int("int"); got != 100 {
			t.Errorf("Int = %d", got)
		}
		if got := r.Int64("int64"); got != 64 {
			t.Errorf("Int64 = %d", got)
		}
		if _, err := r.IntE("string"); !errors.Is(err, ErrNotNumber) {
			t.Errorf("IntE(string) err = %v, want ErrNotNumber", err)
		}
	})

	t.Run("floats", func(t *testing.T) {
		if got := r.Float64("float64"); got != 64.64 {
			t.Errorf("Float64 = %v", got)
		}
		if _, err := r.Float64E("string"); !errors.Is(err, ErrNotNumber) {
			t.Errorf("Float64E(string) err = %v, want ErrNotNumber", err)
		}
	})

	t.Run("bool", func(t *testing.T) {
		cases := map[string]bool{
			"bool_true_txt_test": true,
			"bool_false_txt":     false,
			"bool_true_num_1":    true,
			"bool_true_num_123":  true,
			"bool_false_num_0":   false,
			"missing":            false,
		}
		for field, want := range cases {
			if got := r.Bool(field); got != want {
				t.Errorf("Bool(%q) = %v, want %v", field, got, want)
			}
		}
	})

	t.Run("time", func(t *testing.T) {
		want := time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)
		for _, field := range []string{"timestamp_1", "timestamp_2"} {
			if got := r.Time(field); !got.Equal(want) {
				t.Errorf("Time(%q) = %v, want %v", field, got, want)
			}
		}
		wantDate := time.Date(2006, 1, 2, 0, 0, 0, 0, time.UTC)
		for _, field := range []string{"date_1", "date_2"} {
			if got := r.Time(field); !got.Equal(wantDate) {
				t.Errorf("Time(%q) = %v, want %v", field, got, wantDate)
			}
		}
		if _, err := r.TimeE("time_invalid"); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("TimeE(invalid) err = %v, want ErrUnknownFormat", err)
		}
	})
}

func TestRecordTimeLocation(t *testing.T) {
	loc := time.FixedZone("TEST", 2*60*60) // +02:00

	r := Record{fieldData: map[string]any{"ts": "01/02/2006 15:04:05"}, loc: loc}
	got := r.Time("ts")
	if got.Location() != loc {
		t.Errorf("Time location = %v, want %v", got.Location(), loc)
	}
	if want := time.Date(2006, 1, 2, 15, 4, 5, 0, loc); !got.Equal(want) {
		t.Errorf("Time = %v, want %v", got, want)
	}

	// Explicit override ignores the record's location.
	if utc := r.TimeIn("ts", time.UTC); utc.Location() != time.UTC {
		t.Errorf("TimeIn location = %v, want UTC", utc.Location())
	}

	// No configured location defaults to UTC.
	r2 := Record{fieldData: map[string]any{"ts": "01/02/2006 15:04:05"}}
	if loc := r2.Time("ts").Location(); loc != time.UTC {
		t.Errorf("default location = %v, want UTC", loc)
	}
}

type addressGroup struct {
	String string `fm:"string"`
}

type testRecordStruct struct {
	String      string     `fm:"string"`
	Int         int        `fm:"int"`
	Int8        int8       `fm:"int8"`
	Int64       int64      `fm:"int64"`
	Float64     float64    `fm:"float64"`
	BoolText    bool       `fm:"bool_true_txt_test"`
	BoolNumZero bool       `fm:"bool_false_num_0"`
	Date        time.Time  `fm:"date_1"`
	Timestamp   time.Time  `fm:"timestamp_1"`
	TimeInvalid time.Time  `fm:"time_invalid"`
	TimePointer *time.Time `fm:"timestamp_1"`
	TimePtrZero *time.Time `fm:"time_invalid"`

	Untagged string       // no fm tag: must be left untouched
	Skipped  string       `fm:"-"` // explicitly skipped
	Nested   addressGroup // nested struct: must NOT be recursed into
}

func TestRecordDecode(t *testing.T) {
	value := testRecordStruct{Untagged: "keep", Skipped: "keep"}

	if err := testRecord().Decode(&value); err != nil {
		t.Fatalf("Decode: %v", err)
	}

	wantTS := time.Date(2006, 1, 2, 15, 4, 5, 0, time.UTC)

	checks := []struct {
		name string
		ok   bool
	}{
		{"string", value.String == "string"},
		{"int", value.Int == 100},
		{"int8", value.Int8 == 8},
		{"int64", value.Int64 == 64},
		{"float64", value.Float64 == 64.64},
		{"bool_text", value.BoolText},
		{"bool_num_zero", !value.BoolNumZero},
		{"date", value.Date.Equal(time.Date(2006, 1, 2, 0, 0, 0, 0, time.UTC))},
		{"timestamp", value.Timestamp.Equal(wantTS)},
		{"time_invalid", value.TimeInvalid.IsZero()},
		{"time_pointer", value.TimePointer != nil && value.TimePointer.Equal(wantTS)},
		{"time_pointer_zero", value.TimePtrZero == nil},
		{"untagged_untouched", value.Untagged == "keep"},
		{"skipped_untouched", value.Skipped == "keep"},
		{"nested_not_recursed", value.Nested.String == ""},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s: decoded incorrectly (%+v)", c.name, value)
		}
	}

	// Nested structs are decoded explicitly, not recursively.
	if err := testRecord().Decode(&value.Nested); err != nil {
		t.Fatalf("Decode nested: %v", err)
	}
	if value.Nested.String != "string" {
		t.Errorf("manual nested decode: got %q, want %q", value.Nested.String, "string")
	}
}

func TestRecordDecodeClearsStaleTimePointer(t *testing.T) {
	var value struct {
		T *time.Time `fm:"ts"`
	}

	// First decode: a parseable value sets the pointer.
	if err := (Record{fieldData: map[string]any{"ts": "01/02/2006 15:04:05"}}).Decode(&value); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if value.T == nil {
		t.Fatal("first decode: T is nil, want set")
	}

	// Second decode: an empty value must clear the pointer back to nil rather
	// than leaving the stale value.
	if err := (Record{fieldData: map[string]any{"ts": ""}}).Decode(&value); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if value.T != nil {
		t.Errorf("second decode: T = %v, want nil", value.T)
	}
}

func TestRecordDecodeErrors(t *testing.T) {
	r := testRecord()

	var notPointer testRecordStruct
	if err := r.Decode(notPointer); err == nil {
		t.Error("Decode(struct) = nil, want error")
	}

	var nilPointer *testRecordStruct
	if err := r.Decode(nilPointer); err == nil {
		t.Error("Decode(nil pointer) = nil, want error")
	}

	n := 5
	if err := r.Decode(&n); err == nil {
		t.Error("Decode(*int) = nil, want error")
	}
}

// TestRecordDecodeUnsupportedFieldTypes checks that an `fm` tag on a field
// Decode cannot populate is reported rather than silently skipped, that every
// offending field is named in one error, and that the fields that can decode are
// still populated.
func TestRecordDecodeUnsupportedFieldTypes(t *testing.T) {
	var value struct {
		Supported string       `fm:"string"`
		Uint      uint         `fm:"int"`
		Slice     []string     `fm:"string"`
		Nested    addressGroup `fm:"string"`
		Untagged  uint         // no fm tag: not an error
	}

	err := testRecord().Decode(&value)
	if err == nil {
		t.Fatal("Decode(unsupported field types) = nil, want error")
	}
	for _, name := range []string{"Uint", "Slice", "Nested"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("error does not name field %s: %v", name, err)
		}
	}
	if strings.Contains(err.Error(), "Untagged") {
		t.Errorf("error names untagged field Untagged: %v", err)
	}
	// The nested-struct case is the v3 Map recursion trap, so it carries a hint.
	if !strings.Contains(err.Error(), "not recursive") {
		t.Errorf("error lacks the nested-struct hint: %v", err)
	}
	if value.Supported != "string" {
		t.Errorf("Supported = %q, want %q: decodable fields must still be set", value.Supported, "string")
	}
}

// TestRecordDecodeDefinedType documents that a defined type over a supported
// type does not decode (the type switch matches concrete types) and is reported
// rather than left silently at its zero value.
func TestRecordDecodeDefinedType(t *testing.T) {
	type status string

	var value struct {
		Status status `fm:"string"`
	}

	err := testRecord().Decode(&value)
	if err == nil {
		t.Fatal("Decode(defined type) = nil, want error")
	}
	if strings.Contains(err.Error(), "not recursive") {
		t.Errorf("non-struct field carries the nested-struct hint: %v", err)
	}
}

// TestTimeGetterFormats checks every layout TimeE accepts, in both US and ISO
// form, and confirms a timestamp keeps its time component (it is not truncated
// to a date). Each layout requires a full match — Go's time.Parse errors on
// extra or missing text — so the order of timeFormats is not load-bearing; this
// asserts each value parses to the right instant regardless.
func TestTimeGetterFormats(t *testing.T) {
	loc := time.UTC
	tsWant := time.Date(2026, 6, 23, 15, 4, 5, 0, loc)
	dateWant := time.Date(2026, 6, 23, 0, 0, 0, 0, loc)
	todWant := time.Date(0, 1, 1, 15, 4, 5, 0, loc) // time-only: Go's zero date

	cases := []struct {
		name, value string
		want        time.Time
	}{
		{"US timestamp", "06/23/2026 15:04:05", tsWant},
		{"ISO timestamp space", "2026-06-23 15:04:05", tsWant},
		{"ISO timestamp T", "2026-06-23T15:04:05", tsWant},
		{"US date", "06/23/2026", dateWant},
		{"ISO date", "2026-06-23", dateWant},
		{"time of day", "15:04:05", todWant},
	}
	for _, c := range cases {
		r := Record{loc: loc, fieldData: map[string]any{"f": c.value}}
		got, err := r.TimeE("f")
		if err != nil {
			t.Errorf("%s: TimeE(%q) error: %v", c.name, c.value, err)
			continue
		}
		if !got.Equal(c.want) {
			t.Errorf("%s: TimeE(%q) = %v, want %v", c.name, c.value, got, c.want)
		}
	}
}

// TestTimeGetterErrors covers the values TimeE rejects: an unparseable string, a
// missing field, and a Time value of 24h or more (out of the wall-clock range —
// read those with Duration). Time (the non-E form) returns the zero time.
func TestTimeGetterErrors(t *testing.T) {
	r := Record{fieldData: map[string]any{
		"bad":  "not a date",
		"over": "37:30:00", // >= 24h: a duration, not a clock time
		"num":  float64(5), // a number field, not a date string
	}}
	for _, f := range []string{"bad", "over", "num", "missing"} {
		if _, err := r.TimeE(f); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("TimeE(%q) err = %v, want ErrUnknownFormat", f, err)
		}
		if got := r.Time(f); !got.IsZero() {
			t.Errorf("Time(%q) = %v, want zero time", f, got)
		}
	}
}

func TestDurationGetter(t *testing.T) {
	valid := map[string]time.Duration{
		"00:00:00":  0,
		"15:04:05":  15*time.Hour + 4*time.Minute + 5*time.Second,
		"37:30:00":  37*time.Hour + 30*time.Minute, // exceeds 24h
		"100:00:00": 100 * time.Hour,
		"-01:30:00": -(time.Hour + 30*time.Minute), // negative
		"-00:00:01": -time.Second,
	}
	for v, want := range valid {
		r := Record{fieldData: map[string]any{"f": v}}
		got, err := r.DurationE("f")
		if err != nil {
			t.Errorf("DurationE(%q) error: %v", v, err)
			continue
		}
		if got != want {
			t.Errorf("DurationE(%q) = %v, want %v", v, got, want)
		}
	}

	invalid := []string{
		"",                    // empty
		"12:00",               // too few components
		"12:00:00:00",         // too many components
		"aa:bb:cc",            // non-numeric
		"12:60:00",            // minute out of range
		"12:00:60",            // second out of range
		"2026-06-23",          // a date
		"2026-06-23T15:04:05", // a timestamp
	}
	for _, v := range invalid {
		r := Record{fieldData: map[string]any{"f": v}}
		if _, err := r.DurationE("f"); !errors.Is(err, ErrUnknownFormat) {
			t.Errorf("DurationE(%q) err = %v, want ErrUnknownFormat", v, err)
		}
		if got := r.Duration("f"); got != 0 {
			t.Errorf("Duration(%q) = %v, want 0", v, got)
		}
	}
}

func TestDecodeDuration(t *testing.T) {
	r := Record{fieldData: map[string]any{"worked": "37:30:00"}}
	var got struct {
		Worked time.Duration `fm:"worked"`
	}
	if err := r.Decode(&got); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if want := 37*time.Hour + 30*time.Minute; got.Worked != want {
		t.Errorf("Worked = %v, want %v", got.Worked, want)
	}
}
