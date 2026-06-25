package filemaker

import (
	"errors"
	"reflect"
	"strings"
	"time"
)

// FieldData holds a record's fields keyed by field name. When read back from the
// host, FileMaker number fields decode as float64 and text, date and timestamp
// fields as string.
type FieldData map[string]any

// Record is a single record returned by a read operation (Find). It is a plain,
// immutable value: it holds no reference back to the Client and has no methods
// that touch the host. All of its state is unexported and exposed through
// read-only accessors — ID, ModID, Layout, and the field/portal accessors
// (String, Int, …, Decode, Fields, Portals) — so a returned record cannot be
// mutated. Writes are performed by passing field data to the Client's
// Create/Update methods.
type Record struct {
	id     string
	modID  string
	layout string

	fieldData  map[string]any
	portalData map[string][]map[string]any

	// loc is the time zone used to interpret date/timestamp fields. It is set
	// by the client from its WithLocation option; nil means UTC.
	loc *time.Location
}

// ID returns the record's internal FileMaker record ID, assigned by the host.
func (r Record) ID() string { return r.id }

// ModID returns the record's modification ID, which the host changes on every
// edit. It is the basis for optimistic concurrency (see the Update IfUnchanged
// option).
func (r Record) ModID() string { return r.modID }

// Layout returns the layout the record was read through. It is the default
// target for the record-based writes (Update, Delete, UploadToContainer).
func (r Record) Layout() string { return r.layout }

// Fields returns a copy of the record's raw field values, keyed by field name.
// FileMaker number fields are float64; text, date and timestamp fields are
// string. The result is a copy — mutating it does not affect the record — and
// is nil when the record carries no field data. Use the typed accessors
// (String, Int, …) for individual fields.
func (r Record) Fields() map[string]any {
	return cloneFields(r.fieldData)
}

// Portals returns a copy of the record's portal data, keyed by portal name,
// each value a slice of rows. The result is a deep copy — mutating it (including
// its rows) does not affect the record — and is nil when the record carries no
// portal data.
func (r Record) Portals() map[string][]map[string]any {
	return clonePortalData(r.portalData)
}

// cloneFields returns a copy of a field map. Field values are immutable scalars
// (float64/string), so a shallow copy is both faithful and fully independent. A
// nil source yields nil (preserving the distinction from an empty map).
func cloneFields(src map[string]any) map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// clonePortalData returns a deep copy of portal data: the outer map, each row
// slice and each row map are rebuilt so mutating the result cannot reach the
// record. The leaf values are immutable scalars and are shared. Nil maps and
// slices are preserved as nil so the copy equals the original.
func clonePortalData(src map[string][]map[string]any) map[string][]map[string]any {
	if src == nil {
		return nil
	}
	dst := make(map[string][]map[string]any, len(src))
	for name, rows := range src {
		if rows == nil {
			dst[name] = nil
			continue
		}
		rowsCopy := make([]map[string]any, len(rows))
		for i, row := range rows {
			rowsCopy[i] = cloneFields(row)
		}
		dst[name] = rowsCopy
	}
	return dst
}

// location resolves the record's configured time zone, defaulting to UTC.
func (r Record) location() *time.Location {
	if r.loc != nil {
		return r.loc
	}
	return time.UTC
}

// Has reports whether the record contains the named field. It distinguishes an
// absent field from one present with a zero value (which the typed getters
// cannot).
func (r Record) Has(fieldName string) bool {
	_, ok := r.fieldData[fieldName]
	return ok
}

// Get returns the raw value of a field, or nil if it is absent. FileMaker number
// fields are float64; text, date and timestamp fields are string.
func (r Record) Get(fieldName string) any {
	return r.fieldData[fieldName]
}

// StringE behaves like String but returns ErrNotString if the value is not a string.
func (r Record) StringE(fieldName string) (string, error) {
	if val, ok := r.Get(fieldName).(string); ok {
		return val, nil
	}
	return "", ErrNotString
}

// String returns the field value as a string. The field needs to be a text
// field. Errors are ignored; use StringE to detect them.
func (r Record) String(fieldName string) string {
	s, _ := r.StringE(fieldName)
	return s
}

// StringSliceE behaves like StringSlice but returns ErrNotString if the value
// is not a string.
func (r Record) StringSliceE(fieldName string) ([]string, error) {
	val, err := r.StringE(fieldName)
	if err != nil {
		return nil, err
	}
	if val == "" {
		return nil, nil
	}
	// Normalize CRLF and lone CR to LF, then trim a single trailing line
	// break so a terminating newline does not yield an empty final element.
	val = strings.ReplaceAll(val, "\r\n", "\n")
	val = strings.ReplaceAll(val, "\r", "\n")
	val = strings.TrimSuffix(val, "\n")
	return strings.Split(val, "\n"), nil
}

// StringSlice returns the field value split on line breaks, treating the text
// field as a newline-separated list of values. Carriage returns, line feeds and
// CRLF pairs are all accepted as line breaks. Blank lines between values are
// preserved as empty strings, but a single trailing line break is treated as a
// terminator and does not produce a trailing empty element. An empty field
// yields a nil slice. The field needs to be a text field. Errors are ignored;
// use StringSliceE to detect them.
func (r Record) StringSlice(fieldName string) []string {
	s, _ := r.StringSliceE(fieldName)
	return s
}

// IntE behaves like Int but returns ErrNotNumber if the value is not a number.
func (r Record) IntE(fieldName string) (int, error) {
	if val, ok := r.Get(fieldName).(float64); ok {
		return int(val), nil
	}
	return 0, ErrNotNumber
}

// Int returns the field value as an int. The field needs to be a number field.
func (r Record) Int(fieldName string) int {
	i, _ := r.IntE(fieldName)
	return i
}

// Int64E behaves like Int64 but returns ErrNotNumber if the value is not a number.
func (r Record) Int64E(fieldName string) (int64, error) {
	if val, ok := r.Get(fieldName).(float64); ok {
		return int64(val), nil
	}
	return 0, ErrNotNumber
}

// Int64 returns the field value as an int64. The field needs to be a number field.
func (r Record) Int64(fieldName string) int64 {
	i, _ := r.Int64E(fieldName)
	return i
}

// Float64E behaves like Float64 but returns ErrNotNumber if the value is not a number.
func (r Record) Float64E(fieldName string) (float64, error) {
	if val, ok := r.Get(fieldName).(float64); ok {
		return val, nil
	}
	return 0, ErrNotNumber
}

// Float64 returns the field value as a float64. The field needs to be a number field.
func (r Record) Float64(fieldName string) float64 {
	f, _ := r.Float64E(fieldName)
	return f
}

// Bool parses the field value as a bool: empty fields are false, non-empty text
// fields and number fields greater than 0 are true.
func (r Record) Bool(fieldName string) bool {
	switch val := r.Get(fieldName).(type) {
	case string:
		return len(val) > 0
	case float64:
		return val > 0
	}
	return false
}

// timeFormats are the FileMaker date/timestamp layouts the Time accessors
// recognize, ordered most-specific first so a timestamp is not truncated to a
// date.
var timeFormats = []string{
	"01/02/2006 15:04:05",
	"2006-01-02 15:04:05",
	"01/02/2006",
	"2006-01-02",
}

// TimeInE parses the field value as a time.Time in the given location, returning
// ErrUnknownFormat if it matches none of the supported date/timestamp formats.
func (r Record) TimeInE(fieldName string, loc *time.Location) (time.Time, error) {
	data := r.String(fieldName)
	for _, layout := range timeFormats {
		if t, err := time.ParseInLocation(layout, data, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, ErrUnknownFormat
}

// TimeIn parses the field value as a time.Time in the given location. Errors are
// ignored; use TimeInE to detect them.
func (r Record) TimeIn(fieldName string, loc *time.Location) time.Time {
	t, _ := r.TimeInE(fieldName, loc)
	return t
}

// TimeE parses the field value using the record's configured location (set via
// the client's WithLocation option; UTC by default).
func (r Record) TimeE(fieldName string) (time.Time, error) {
	return r.TimeInE(fieldName, r.location())
}

// Time parses the field value using the record's configured location. Errors are
// ignored; use TimeE to detect them.
func (r Record) Time(fieldName string) time.Time {
	return r.TimeIn(fieldName, r.location())
}

// Decode populates obj's fields from the record, matching each struct field's
// `fm` tag to a record field name. obj must be a non-nil pointer to a struct.
//
// Decode is NOT recursive. FileMaker records are flat, so Decode maps only the
// fields of the struct passed to it; nested struct fields are left untouched. To
// populate a nested struct, call Decode on it directly:
//
//	rec.Decode(&customer)
//	rec.Decode(&customer.Address)
//
// (v3's Map decoded nested structs automatically; v4 does not — see the v4
// migration notes.)
//
// Only fields with a non-empty `fm` tag are touched; untagged fields and fields
// tagged `fm:"-"` are left as-is. Decoding is lenient per field: a missing or
// empty field leaves the struct field at its zero value. time.Time fields use
// the record's configured location; a *time.Time field is set to a pointer when
// the value parses to a non-zero time, and to nil otherwise (clearing any value
// from a previous Decode).
//
// An error is returned only for structural misuse — obj not being a non-nil
// pointer to a struct.
//
// Supported field types: string, int, int8, int16, int32, int64, float32,
// float64, bool, time.Time, *time.Time.
func (r Record) Decode(obj any) error {
	v := reflect.ValueOf(obj)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("filemaker: decode requires a non-nil pointer to a struct")
	}
	v = v.Elem()
	if v.Kind() != reflect.Struct {
		return errors.New("filemaker: decode requires a pointer to a struct")
	}

	vType := v.Type()
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.CanSet() {
			continue
		}

		tag := vType.Field(i).Tag.Get("fm")
		if tag == "" || tag == "-" {
			continue
		}

		switch field.Interface().(type) {
		case string:
			field.SetString(r.String(tag))
		case int, int8, int16, int32, int64:
			field.SetInt(r.Int64(tag))
		case float32, float64:
			field.SetFloat(r.Float64(tag))
		case bool:
			field.SetBool(r.Bool(tag))
		case time.Time:
			field.Set(reflect.ValueOf(r.Time(tag)))
		case *time.Time:
			// Assign unconditionally so the field reflects this record: a
			// fresh pointer for a parseable value, nil otherwise (clearing any
			// value left by a previous Decode).
			if parsed := r.Time(tag); !parsed.IsZero() {
				field.Set(reflect.ValueOf(&parsed))
			} else {
				field.Set(reflect.Zero(field.Type()))
			}
		}
	}
	return nil
}
