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
