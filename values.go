package filemaker

import (
	"encoding/json"
	"fmt"
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
//	    "Alarm":   filemaker.Time(alarmTime),    	// -> "15:04:05"
//	    "Worked":  filemaker.Duration(elapsed),  	// -> "37:30:00"
//	})
//
// Date and Timestamp render in US format by default. A client built with
// WithDateFormat(DateFormatISO) rewrites them to ISO 8601 at request time and
// tells the host to interpret the input accordingly; the wrappers themselves are
// unchanged, so this file's output is the default (US) representation.

// fmDate formats t as a FileMaker date in the given format (US MM/DD/YYYY or ISO
// YYYY-MM-DD). The zero value dateFormatUnset is treated as US.
func fmDate(t time.Time, f DateFormat) string {
	if f == DateFormatISO {
		return t.Format("2006-01-02")
	}
	return t.Format("01/02/2006")
}

// fmTimestamp formats t as a FileMaker timestamp. The ISO form uses a space
// separator, not a "T": the host rejects a "T" on input (even though it emits
// one when returning dateformats=2 values).
func fmTimestamp(t time.Time, f DateFormat) string {
	if f == DateFormatISO {
		return t.Format("2006-01-02 15:04:05")
	}
	return t.Format("01/02/2006 15:04:05")
}

// fmTimeOfDay formats t's clock portion as a FileMaker time (HH:MM:SS). It does
// not depend on the date format.
func fmTimeOfDay(t time.Time) string {
	return t.Format("15:04:05")
}

// fmDuration formats d as a FileMaker time-of-day duration ([-]HH:MM:SS), with
// hours allowed to exceed 24 — FileMaker Time fields can hold elapsed durations.
func fmDuration(d time.Duration) string {
	sign := ""
	if d < 0 {
		sign = "-"
		d = -d
	}
	total := int64(d / time.Second)
	return fmt.Sprintf("%s%02d:%02d:%02d", sign, total/3600, (total%3600)/60, total%60)
}

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

// Date renders a time.Time as a FileMaker date, in US format (MM/DD/YYYY) by
// default; a client with WithDateFormat(DateFormatISO) rewrites it to ISO
// (YYYY-MM-DD) at request time. The zero time marshals to an empty string, which
// clears the field.
type Date time.Time

// MarshalJSON implements json.Marshaler.
func (d Date) MarshalJSON() ([]byte, error) {
	t := time.Time(d)
	if t.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(fmDate(t, DateFormatUS))
}

// Timestamp renders a time.Time as a FileMaker timestamp, in US format
// (MM/DD/YYYY HH:MM:SS) by default; a client with WithDateFormat(DateFormatISO)
// rewrites it to ISO at request time. The zero time marshals to an empty string,
// which clears the field.
type Timestamp time.Time

// MarshalJSON implements json.Marshaler.
func (ts Timestamp) MarshalJSON() ([]byte, error) {
	t := time.Time(ts)
	if t.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(fmTimestamp(t, DateFormatUS))
}

// Time renders the clock portion of a time.Time as a FileMaker time (HH:MM:SS),
// for writing to a Time field. The zero time marshals to an empty string, which
// clears the field. For an elapsed duration (e.g. 24 hours or more), use
// Duration instead.
type Time time.Time

// MarshalJSON implements json.Marshaler.
func (tm Time) MarshalJSON() ([]byte, error) {
	t := time.Time(tm)
	if t.IsZero() {
		return []byte(`""`), nil
	}
	return json.Marshal(fmTimeOfDay(t))
}

// Duration renders a time.Duration as a FileMaker time ([-]HH:MM:SS) for writing
// to a Time field. Unlike Time it can represent 24 hours or more, and negative
// values. A zero duration writes "00:00:00" (it has no clear-the-field sentinel;
// pass an empty string to clear).
type Duration time.Duration

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(fmDuration(time.Duration(d)))
}
