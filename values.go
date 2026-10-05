package filemaker

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// This file provides optional typed value wrappers for building FieldData. They
// are opt-in: Create/Update marshal FieldData faithfully, and any value that
// implements json.Marshaler (like these) is converted on the way out. Plain
// strings and numbers need no wrapper: Go integers are sent as their exact
// digits (see Number for why), and floats as JSON numbers, which the host stores
// exactly for decimals of up to 15 significant digits. Number carries decimals
// that need more, and integers beyond int64.
//
//	c.Create(ctx, "People", filemaker.FieldData{
//	    "Name":    "Mark",                       	// raw, as-is
//	    "Total":   filemaker.Number("12345678901234.567"),	// -> exact digits
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
// YYYY-MM-DD). Anything but DateFormatISO renders as US, so the zero value
// needs no special case.
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

// Number is a FileMaker number held as its exact decimal text, in JSON number
// syntax ("42", "-3.5", "1.2e+29"). FileMaker numbers carry far more digits than
// a float64, so records return number fields as Number rather than rounding
// them, and a Number written back keeps every digit.
//
// A Number is written as a JSON string of its digits, not as a JSON number: the
// host parses a JSON-number input as a double before storing it, which is exact
// only up to 15 significant digits, but converts a string input itself and
// stores it exactly. The stored value is an ordinary number either way — it
// sorts, finds and calculates as one. Go integer values in FieldData are sent
// the same way, and a float64 is stored as the value it holds, which is exact
// for decimals of up to 15 significant digits. So a Number is needed only for
// decimals with more digits than that, integers beyond int64, and values that
// already exist as exact decimal text (from a decimal package, or parsed
// input), where converting to float64 first would round them. For
// arbitrary-precision arithmetic, convert String() with math/big or a decimal
// package.
//
// The host sends a number field's stored text as it was entered, not in a
// canonical form: "7.0" and "1e3" arrive as written. In a file whose locale uses
// a decimal comma, the host rewrites the comma to a point ("0,7" arrives as
// "0.7") but passes a point through unchanged, although FileMaker ignores it
// there: "7.0" typed into such a file is 70 to FileMaker and 7 to Float64.
// Entered text that is not in JSON number form, such as "007", arrives as a
// string rather than a Number, so the numeric accessors report ErrNotNumber
// for it.
//
// The zero value writes an empty string, which clears the field. Text that is
// not a JSON number (hex, a leading "+", surrounding spaces) fails to marshal
// rather than being stored as text.
type Number string

// String returns the number's exact decimal text.
func (n Number) String() string { return string(n) }

// Int64 returns the number as an int64, exactly. It accepts only integer
// notation ("42", "-7"): a number written with a fractional part or an exponent
// ("3.7", "7.0", "1e3") returns ErrNotInteger, an integer beyond int64 returns
// ErrOutOfRange, and text that is not a number returns ErrNotNumber. The result
// is 0 whenever the error is non-nil. For other notations, parse String()
// yourself, for example with math/big's Rat.SetString.
func (n Number) Int64() (int64, error) {
	if !isJSONNumber(string(n)) {
		return 0, ErrNotNumber
	}
	i, err := strconv.ParseInt(string(n), 10, 64)
	switch {
	case err == nil:
		return i, nil
	case errors.Is(err, strconv.ErrRange):
		return 0, ErrOutOfRange
	default: // a valid JSON number that is not in integer notation
		return 0, ErrNotInteger
	}
}

// Float64 returns the number as the nearest float64. It returns ErrOutOfRange
// if the magnitude is beyond float64, and ErrNotNumber if n is not a number.
func (n Number) Float64() (float64, error) {
	if !isJSONNumber(string(n)) {
		return 0, ErrNotNumber
	}
	f, err := strconv.ParseFloat(string(n), 64)
	if err != nil {
		// Only a range error is possible here, since isJSONNumber checked the
		// syntax, and ParseFloat reports only overflow: a magnitude too small for
		// float64 parses to its nearest value (0 or a subnormal) without error,
		// which is the float64 reading of the number.
		return 0, ErrOutOfRange
	}
	return f, nil
}

// MarshalJSON implements json.Marshaler, writing the digits as a JSON string
// (see Number).
func (n Number) MarshalJSON() ([]byte, error) {
	if n == "" {
		return []byte(`""`), nil
	}
	if !isJSONNumber(string(n)) {
		return nil, fmt.Errorf("%w: %q", ErrNotNumber, string(n))
	}
	// JSON number syntax needs no escaping inside a string.
	return []byte(`"` + string(n) + `"`), nil
}

// positive reports whether n is a number greater than zero: it has no minus
// sign and a nonzero digit before any exponent. It reads the text rather than
// parsing it, so it holds at any magnitude.
func (n Number) positive() bool {
	s := string(n)
	if !isJSONNumber(s) || s[0] == '-' {
		return false
	}
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		s = s[:i]
	}
	return strings.ContainsAny(s, "123456789")
}

// isJSONNumber reports whether s is a number in JSON syntax, the form the host
// returns and the only form Number writes. It rejects what json.Valid accepts
// around a number (other value types, surrounding whitespace) by checking that
// s starts with a sign or digit and ends with a digit.
func isJSONNumber(s string) bool {
	if s == "" || (s[0] != '-' && !isDigit(s[0])) || !isDigit(s[len(s)-1]) {
		return false
	}
	return json.Valid([]byte(s))
}

func isDigit(c byte) bool { return '0' <= c && c <= '9' }

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
