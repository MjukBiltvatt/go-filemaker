package filemaker

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestBoolMarshalJSON(t *testing.T) {
	cases := map[Bool]string{
		Bool(true):  "1",
		Bool(false): "0",
	}
	for in, want := range cases {
		got, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("Marshal(%v): %v", bool(in), err)
		}
		if string(got) != want {
			t.Errorf("Bool(%v) = %s, want %s", bool(in), got, want)
		}
	}
}

func TestDateMarshalJSON(t *testing.T) {
	got, err := json.Marshal(Date(time.Date(1990, 6, 23, 14, 5, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(got) != `"06/23/1990"` {
		t.Errorf("Date = %s, want \"06/23/1990\"", got)
	}

	zero, err := json.Marshal(Date(time.Time{}))
	if err != nil {
		t.Fatalf("Marshal zero: %v", err)
	}
	if string(zero) != `""` {
		t.Errorf("zero Date = %s, want empty string", zero)
	}
}

func TestTimestampMarshalJSON(t *testing.T) {
	got, err := json.Marshal(Timestamp(time.Date(2026, 6, 23, 14, 5, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(got) != `"06/23/2026 14:05:00"` {
		t.Errorf("Timestamp = %s, want \"06/23/2026 14:05:00\"", got)
	}

	zero, err := json.Marshal(Timestamp(time.Time{}))
	if err != nil {
		t.Fatalf("Marshal zero: %v", err)
	}
	if string(zero) != `""` {
		t.Errorf("zero Timestamp = %s, want empty string", zero)
	}
}

func TestTimeMarshalJSON(t *testing.T) {
	got, err := json.Marshal(Time(time.Date(2025, 6, 23, 15, 4, 5, 0, time.UTC)))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(got) != `"15:04:05"` {
		t.Errorf("Time = %s, want \"15:04:05\"", got)
	}

	zero, err := json.Marshal(Time(time.Time{}))
	if err != nil {
		t.Fatalf("Marshal zero: %v", err)
	}
	if string(zero) != `""` {
		t.Errorf("zero Time = %s, want empty string", zero)
	}
}

func TestDurationMarshalJSON(t *testing.T) {
	cases := map[time.Duration]string{
		time.Hour + 2*time.Minute + 3*time.Second:    `"01:02:03"`,
		37*time.Hour + 30*time.Minute:                `"37:30:00"`, // exceeds 24h
		-(time.Hour + 2*time.Minute + 3*time.Second): `"-01:02:03"`,
		0: `"00:00:00"`,
	}
	for in, want := range cases {
		got, err := json.Marshal(Duration(in))
		if err != nil {
			t.Fatalf("Marshal(%v): %v", in, err)
		}
		if string(got) != want {
			t.Errorf("Duration(%v) = %s, want %s", in, got, want)
		}
	}
}

// TestNumberMarshalJSON checks that a Number is written as a JSON string of its
// exact digits: the host stores a JSON-number input as a double (exact only up to
// 15 significant digits) but converts a string input itself, keeping every
// digit.
func TestNumberMarshalJSON(t *testing.T) {
	cases := map[Number]string{
		"20260924123456789":              `"20260924123456789"`,
		"12345678901234.567":             `"12345678901234.567"`,
		"123456789012345678901234567890": `"123456789012345678901234567890"`,
		"-3.5":                           `"-3.5"`,
		"1e3":                            `"1e3"`,
		"":                               `""`, // clears the field, like the zero Date
	}
	for in, want := range cases {
		got, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("Marshal(%q): %v", string(in), err)
		}
		if string(got) != want {
			t.Errorf("Number(%q) = %s, want %s", string(in), got, want)
		}
	}
}

// TestNumberMarshalJSONInvalid checks that text that is not a JSON number is
// refused rather than sent as a string the host would store as text.
func TestNumberMarshalJSONInvalid(t *testing.T) {
	for _, in := range []Number{"abc", " 7", "7 ", "0x10", "1/3", "+5", "07", ".5", "5.", "NaN", "Inf"} {
		if got, err := json.Marshal(in); err == nil {
			t.Errorf("Marshal(Number(%q)) = %s, want an error", string(in), got)
		}
	}
}

func TestNumberInt64(t *testing.T) {
	cases := []struct {
		in      Number
		want    int64
		wantErr error
	}{
		{"42", 42, nil},
		{"-42", -42, nil},
		{"0", 0, nil},
		{"-0", 0, nil},
		{"9007199254740993", 9007199254740993, nil}, // 2^53 + 1: past float64
		{"9223372036854775807", 9223372036854775807, nil},
		{"-9223372036854775808", -9223372036854775808, nil},
		{"9223372036854775808", 0, ErrOutOfRange},
		{"-9223372036854775809", 0, ErrOutOfRange},
		{"123456789012345678901234567890", 0, ErrOutOfRange},
		// Only integer notation is read: a fraction or an exponent is
		// ErrNotInteger even when the value is whole.
		{"3.7", 0, ErrNotInteger},
		{"-0.5", 0, ErrNotInteger},
		{"7.0", 0, ErrNotInteger},
		{"1e3", 0, ErrNotInteger},
		{"1E3", 0, ErrNotInteger},
		{"1.2345678901234568e+29", 0, ErrNotInteger},
		{"1e99999999999999999999", 0, ErrNotInteger},
		{"", 0, ErrNotNumber},
		{"abc", 0, ErrNotNumber},
		{"007", 0, ErrNotNumber}, // not a JSON number, though ParseInt reads it
		{"+5", 0, ErrNotNumber},  // likewise
	}
	for _, c := range cases {
		got, err := c.in.Int64()
		if !errors.Is(err, c.wantErr) || (c.wantErr == nil && err != nil) {
			t.Errorf("Number(%q).Int64() error = %v, want %v", string(c.in), err, c.wantErr)
		}
		if got != c.want {
			t.Errorf("Number(%q).Int64() = %d, want %d", string(c.in), got, c.want)
		}
	}
}

func TestNumberFloat64(t *testing.T) {
	cases := []struct {
		in      Number
		want    float64
		wantErr error
	}{
		{"3.5", 3.5, nil},
		{"-42", -42, nil},
		{"12345678901234.567", 12345678901234.567, nil}, // nearest float64
		{"1e3", 1000, nil},
		{"1e400", 0, ErrOutOfRange},
		{"-1e400", 0, ErrOutOfRange},
		{"1e-400", 0, nil}, // too small: rounds to 0, not an error
		{"", 0, ErrNotNumber},
		{"abc", 0, ErrNotNumber},
	}
	for _, c := range cases {
		got, err := c.in.Float64()
		if !errors.Is(err, c.wantErr) || (c.wantErr == nil && err != nil) {
			t.Errorf("Number(%q).Float64() error = %v, want %v", string(c.in), err, c.wantErr)
		}
		if got != c.want {
			t.Errorf("Number(%q).Float64() = %v, want %v", string(c.in), got, c.want)
		}
	}
}

func TestNumberString(t *testing.T) {
	if got := Number("123456789012345678901234567890").String(); got != "123456789012345678901234567890" {
		t.Errorf("String() = %q", got)
	}
}
