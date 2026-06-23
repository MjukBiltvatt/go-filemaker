package filemaker

import (
	"encoding/json"
	"time"
)

// This file provides optional typed value wrappers for building FieldData. They
// are opt-in: Create/Update marshal FieldData faithfully, and any value that
// implements json.Marshaler (like these) is converted on the way out. Plain
// strings and numbers need no wrapper.
//
//	c.Create(ctx, "People", filemaker.FieldData{
//	    "Name":    "Mark",                       	// raw, as-is
//	    "Active":  filemaker.Bool(isActive),     	// -> 1 / 0
//	    "DOB":     filemaker.Date(birthday),     	// -> "06/23/1990"
//	    "Created": filemaker.Timestamp(time.Now()),	// -> "06/23/1990 15:04:05"
//	})

// Bool renders a Go bool as FileMaker's numeric 1/0. FileMaker has no boolean
// type; booleans are stored as 1/0 in number fields.
type Bool bool

// MarshalJSON implements json.Marshaler.
func (b Bool) MarshalJSON() ([]byte, error) {
	if b {
		return []byte("1"), nil
	}
	return []byte("0"), nil
}

// Date renders a time.Time as a FileMaker date (MM/DD/YYYY). The zero time
// marshals to an empty string, which clears the field.
type Date time.Time

// MarshalJSON implements json.Marshaler.
func (d Date) MarshalJSON() ([]byte, error) {
	t := time.Time(d)
	if t.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(t.Format("01/02/2006"))
}

// Timestamp renders a time.Time as a FileMaker timestamp (MM/DD/YYYY HH:MM:SS).
// The zero time marshals to an empty string, which clears the field.
type Timestamp time.Time

// MarshalJSON implements json.Marshaler.
func (ts Timestamp) MarshalJSON() ([]byte, error) {
	t := time.Time(ts)
	if t.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(t.Format("01/02/2006 15:04:05"))
}
