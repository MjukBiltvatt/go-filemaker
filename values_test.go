package filemaker

import (
	"encoding/json"
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

// TestMarshalRecordBodyDateFormat checks that the body carries a dateformats
// parameter and reformats Date/Timestamp values only when a format is set, and
// is unchanged (no parameter, US values) when unset.
func TestMarshalRecordBodyDateFormat(t *testing.T) {
	fields := FieldData{
		"DOB":     Date(time.Date(1990, 6, 23, 0, 0, 0, 0, time.UTC)),
		"Created": Timestamp(time.Date(2026, 6, 23, 14, 5, 0, 0, time.UTC)),
	}
	us, iso := DateFormatUS, DateFormatISO
	cases := []struct {
		name   string
		format *DateFormat
		want   string
	}{
		{"unset", nil, `{"fieldData":{"Created":"06/23/2026 14:05:00","DOB":"06/23/1990"}}`},
		{"US", &us, `{"dateformats":0,"fieldData":{"Created":"06/23/2026 14:05:00","DOB":"06/23/1990"}}`},
		{"ISO", &iso, `{"dateformats":2,"fieldData":{"Created":"2026-06-23 14:05:00","DOB":"1990-06-23"}}`},
	}
	for _, c := range cases {
		body, err := marshalRecordBody(fields, recordConfig{}, c.format)
		if err != nil {
			t.Fatalf("%s: marshalRecordBody: %v", c.name, err)
		}
		if string(body) != c.want {
			t.Errorf("%s:\n got: %s\nwant: %s", c.name, body, c.want)
		}
	}
}

func TestFieldDataWithTypedValues(t *testing.T) {
	body, err := marshalRecordBody(FieldData{
		"Active":  Bool(true),
		"DOB":     Date(time.Date(1990, 6, 23, 0, 0, 0, 0, time.UTC)),
		"Created": Timestamp(time.Date(2026, 6, 23, 14, 5, 0, 0, time.UTC)),
		"Name":    "Mark",
	}, recordConfig{}, nil)
	if err != nil {
		t.Fatalf("marshalRecordBody: %v", err)
	}
	want := `{"fieldData":{"Active":1,"Created":"06/23/2026 14:05:00","DOB":"06/23/1990","Name":"Mark"}}`
	if string(body) != want {
		t.Errorf("got:  %s\nwant: %s", body, want)
	}
}
