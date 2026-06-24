package filemaker

import (
	"errors"
	"testing"
	"time"
)

func testRecord() Record {
	return Record{
		Layout: "People",
		FieldData: FieldData{
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
		},
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

	r := Record{FieldData: FieldData{"ts": "01/02/2006 15:04:05"}, loc: loc}
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
	r2 := Record{FieldData: FieldData{"ts": "01/02/2006 15:04:05"}}
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
	if err := (Record{FieldData: FieldData{"ts": "01/02/2006 15:04:05"}}).Decode(&value); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if value.T == nil {
		t.Fatal("first decode: T is nil, want set")
	}

	// Second decode: an empty value must clear the pointer back to nil rather
	// than leaving the stale value.
	if err := (Record{FieldData: FieldData{"ts": ""}}).Decode(&value); err != nil {
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
