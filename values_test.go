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

func TestFieldDataWithTypedValues(t *testing.T) {
	body, err := marshalFieldData(FieldData{
		"Active":  Bool(true),
		"DOB":     Date(time.Date(1990, 6, 23, 0, 0, 0, 0, time.UTC)),
		"Created": Timestamp(time.Date(2026, 6, 23, 14, 5, 0, 0, time.UTC)),
		"Name":    "Mark",
	})
	if err != nil {
		t.Fatalf("marshalFieldData: %v", err)
	}
	want := `{"fieldData":{"Active":1,"Created":"06/23/2026 14:05:00","DOB":"06/23/1990","Name":"Mark"}}`
	if string(body) != want {
		t.Errorf("got:  %s\nwant: %s", body, want)
	}
}
