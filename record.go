package filemaker

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"strconv"
	"strings"
	"time"
)

// FieldData holds a record's fields keyed by field name.
//
// When read back from the host, number fields are Number, holding the host's
// exact digits, and text, date, timestamp and time fields are string. An empty
// field of any type is the empty string, and so is a number field holding text
// a user typed into it: FileMaker keeps such text as entered.
//
// When writing, Go integers and Number values are sent as their exact digits,
// floats as JSON numbers, and strings as-is; the wrappers in this package (Bool,
// Date, …) render Go types in FileMaker's formats. See Number for why integers
// are not sent as JSON numbers.
type FieldData map[string]any

// PortalData holds portal (related) records keyed by portal name, each value a
// slice of rows. It is both what Record.Portals returns and what the
// WithPortalData update option accepts, so portal data read from a record can be
// edited and written back unchanged.
//
// Within a row, field values are keyed by their fully qualified name
// ("TableOccurrence::FieldName"). When writing, a row that carries a record ID
// (a plain "recordId" key, optionally with a plain "modId" for optimistic
// locking) edits that existing related record; a row without one is added as a
// new related record. The record ID is not table-occurrence qualified like the
// field values are — "TableOccurrence::recordId" is read as a field and rejected
// (code 102). See the Claris Data API guide's "Edit record" page for the wire
// format.
type PortalData map[string][]map[string]any

// PortalDataInfo is the host's account of one portal on a returned record, as
// reported by Record.PortalDataInfo: the table occurrence the portal shows, how
// many related records it holds (FoundCount), and how many of those the
// response carries (ReturnedCount). The two differ when the portal's row count,
// WithPortalLimit, or WithPortalOffset leaves rows out, so FoundCount is what a
// caller pages a portal against.
type PortalDataInfo struct {
	Database      string `json:"database"`
	Table         string `json:"table"`
	FoundCount    int    `json:"foundCount"`
	ReturnedCount int    `json:"returnedCount"`
}

// Record is a single record returned by a read operation (Find). It is a plain,
// immutable value: it holds no reference back to the Client and has no methods
// that touch the host. All of its state is unexported and exposed through
// read-only accessors — ID, ModID, Layout, and the field/portal accessors
// (String, Int, …, Decode, Fields, Portals, PortalDataInfo) — so a returned
// record cannot be mutated. Writes are performed by passing field data to the
// Client's Create/Update methods.
type Record struct {
	id     string
	modID  string
	layout string

	fieldData  map[string]any
	portalData map[string][]map[string]any
	portalInfo map[string]PortalDataInfo

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

// Fields returns a copy of the record's raw field values, keyed by field name,
// typed as FieldData describes (Number for numbers, string otherwise). The result is a copy — mutating it does not affect the record — and
// is nil when the record carries no field data. Use the typed accessors
// (String, Int, …) for individual fields.
func (r Record) Fields() FieldData {
	return cloneFields(r.fieldData)
}

// Portals returns a copy of the record's portal data, keyed by portal name,
// each value a slice of rows. The result is a deep copy — mutating it (including
// its rows) does not affect the record — and is nil when the record carries no
// portal data.
func (r Record) Portals() PortalData {
	return clonePortalData(r.portalData)
}

// PortalDataInfo returns the host's account of each portal on the record, keyed
// by the same portal names as Portals: the portal's object name when it has one,
// otherwise its table-occurrence name. It has an entry for exactly the portals
// Portals has — WithPortals omits a portal from both — including a portal with
// no rows. The result is a copy, and is nil when the host reports no portal
// information.
func (r Record) PortalDataInfo() map[string]PortalDataInfo {
	return maps.Clone(r.portalInfo)
}

// cloneFields returns a copy of a field map. Field values are immutable scalars
// (Number/string), so a shallow copy is both faithful and fully independent. A
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
func clonePortalData(src map[string][]map[string]any) PortalData {
	if src == nil {
		return nil
	}
	dst := make(PortalData, len(src))
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

// Get returns the raw value of a field, or nil if it is absent. Values are typed
// as FieldData describes: Number for numbers, string otherwise.
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

// NumberE behaves like Number but returns ErrNotNumber if the value is not a
// number: text (including an empty field), or an absent field.
func (r Record) NumberE(fieldName string) (Number, error) {
	if val, ok := r.Get(fieldName).(Number); ok {
		return val, nil
	}
	return "", ErrNotNumber
}

// Number returns the field value as a Number, with the host's exact digits. Use
// it for values the other numeric accessors cannot hold exactly: integers beyond
// int64 and decimals beyond float64's precision. The field needs to be a number
// field. Errors are ignored; use NumberE to detect them.
func (r Record) Number(fieldName string) Number {
	n, _ := r.NumberE(fieldName)
	return n
}

// IntE behaves like Int but returns an error instead of 0: ErrNotNumber if the
// value is not a number, ErrNotInteger if it is not written as an integer (a
// fractional part or exponent notation; see Number.Int64), and ErrOutOfRange if
// it does not fit an int.
func (r Record) IntE(fieldName string) (int, error) {
	i, err := r.Int64E(fieldName)
	if err != nil {
		return 0, err
	}
	if int64(int(i)) != i {
		return 0, ErrOutOfRange
	}
	return int(i), nil
}

// Int returns the field value as an int. The field needs to be a number field
// holding a value in integer notation; any other value, including a fraction,
// is not truncated but yields 0. Errors are ignored; use IntE to detect them.
func (r Record) Int(fieldName string) int {
	i, _ := r.IntE(fieldName)
	return i
}

// Int64E behaves like Int64 but returns an error instead of 0: ErrNotNumber if
// the value is not a number, ErrNotInteger if it is not written as an integer
// (a fractional part or exponent notation; see Number.Int64), and ErrOutOfRange
// if it does not fit an int64.
func (r Record) Int64E(fieldName string) (int64, error) {
	n, err := r.NumberE(fieldName)
	if err != nil {
		return 0, err
	}
	return n.Int64()
}

// Int64 returns the field value as an int64, exactly. The field needs to be a
// number field holding a value in integer notation; any other value, including
// a fraction, is not truncated but yields 0. Errors are ignored; use Int64E to
// detect them.
func (r Record) Int64(fieldName string) int64 {
	i, _ := r.Int64E(fieldName)
	return i
}

// Float64E behaves like Float64 but returns an error instead of 0: ErrNotNumber
// if the value is not a number, and ErrOutOfRange if its magnitude is beyond
// float64.
func (r Record) Float64E(fieldName string) (float64, error) {
	n, err := r.NumberE(fieldName)
	if err != nil {
		return 0, err
	}
	return n.Float64()
}

// Float64 returns the field value as the nearest float64, which is exact only up
// to 15 significant digits; use Number to keep longer values exact.
// The field needs to be a number field. Errors are ignored; use Float64E to
// detect them.
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
	case Number:
		return val.positive()
	}
	return false
}

// timeFormats are the FileMaker date, timestamp and time-of-day layouts the Time
// accessors recognize, ordered most-specific first so a timestamp is not
// truncated to a date. Both US and ISO forms are accepted, including the
// "T"-separated ISO timestamp the host emits for dateformats=2 reads, so the
// accessors parse a value regardless of the format it was written in.
var timeFormats = []string{
	"01/02/2006 15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02T15:04:05",
	"01/02/2006",
	"2006-01-02",
	"15:04:05",
}

// TimeInE parses the field value as a time.Time in the given location, returning
// ErrUnknownFormat if it matches none of the supported date/timestamp/time
// formats. A Time field holding 24 hours or more (an elapsed duration rather
// than a clock time) is out of the wall-clock range and will not parse here; read
// such a field with Duration instead.
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

// DurationE parses the field value as a time.Duration, for a FileMaker Time
// field returned as a clock string ([-]HH:MM:SS). Unlike Time it represents the
// value as elapsed time, so it handles 24 hours or more and negative values. It
// returns ErrUnknownFormat if the value is not such a string.
func (r Record) DurationE(fieldName string) (time.Duration, error) {
	return parseFMDuration(r.String(fieldName))
}

// Duration parses the field value as a time.Duration. Errors are ignored; use
// DurationE to detect them.
func (r Record) Duration(fieldName string) time.Duration {
	d, _ := r.DurationE(fieldName)
	return d
}

// parseFMDuration parses a FileMaker time clock string ([-]H[H…]:MM:SS) into a
// time.Duration. Hours may exceed 24; minutes and seconds must be 0–59.
func parseFMDuration(s string) (time.Duration, error) {
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	}
	parts := strings.Split(s, ":")
	if len(parts) != 3 {
		return 0, ErrUnknownFormat
	}
	h, errH := strconv.Atoi(parts[0])
	m, errM := strconv.Atoi(parts[1])
	sec, errS := strconv.Atoi(parts[2])
	if errH != nil || errM != nil || errS != nil || h < 0 || m < 0 || m > 59 || sec < 0 || sec > 59 {
		return 0, ErrUnknownFormat
	}
	d := time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second
	if neg {
		d = -d
	}
	return d, nil
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
// tagged `fm:"-"` are left as-is. Each tagged field is set from this record
// alone: it starts from its zero value, so nothing from an earlier Decode into
// the same struct lingers.
//
//   - An empty field (which the host sends as "" whatever its type) decodes to
//     the zero value — nil for *time.Time — without error.
//   - A field the record does not have is an error wrapping ErrMissingField,
//     unless the tag carries the optional option (`fm:"Notes,optional"`), in
//     which case it decodes to the zero value.
//   - A value that cannot be converted to the field's type is an error wrapping
//     the accessor's error: ErrNotNumber, ErrNotInteger, ErrOutOfRange (also
//     for a value that overflows int8…int32 or float32), ErrUnknownFormat for a
//     date, time or duration that does not parse. The field is left at its zero
//     value.
//   - A number field decodes into a string field as its exact text.
//
// time.Time fields use the record's configured location; bool fields follow
// Bool, which accepts any value.
//
// Decode fills every field it can and reports all the fields it cannot in one
// error, in struct field order, alongside any struct-definition faults: an
// `fm`-tagged field whose type is not supported (which depends on the struct
// alone, so it surfaces on the first decode) or an unknown tag option. Each
// failure names the struct field and its tag, and the error matches each cause
// with errors.Is. For a record returned by the host, the error also names the
// record and its layout. A caller that prefers leniency can log the error and
// use the struct, which holds everything that decoded. Note that a defined type
// over a supported type (type Status string) is not itself supported.
//
// Supported field types: string, int, int8, int16, int32, int64, float32,
// float64, Number, bool, time.Duration, time.Time, *time.Time.
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
	var errs []error
	for i := 0; i < v.NumField(); i++ {
		field := v.Field(i)
		if !field.CanSet() {
			continue
		}
		sf := vType.Field(i)
		tag := sf.Tag.Get("fm")
		if tag == "" || tag == "-" {
			continue
		}
		if err := r.decodeField(field, tag); err != nil {
			errs = append(errs, &fieldError{field: sf.Name, tag: tag, err: err})
		}
	}
	if len(errs) == 0 {
		return nil
	}
	err := &decodeError{errs: errs}
	if r.id == "" {
		// Not a record the host returned (a zero or hand-built Record): there
		// is no record to name.
		return err
	}
	return recordErr(err, r.layout, r.id)
}

// decodeField sets one struct field from the record field its `fm` tag names
// (see Decode for the rules), returning why it could not.
func (r Record) decodeField(field reflect.Value, tag string) error {
	name, opts, _ := strings.Cut(tag, ",")
	optional := false
	if opts != "" {
		for _, opt := range strings.Split(opts, ",") {
			if opt != "optional" {
				return fmt.Errorf("unknown tag option %q", opt)
			}
			optional = true
		}
	}
	if !decodable(field.Type()) {
		return unsupportedTypeError(field.Type())
	}

	field.Set(reflect.Zero(field.Type()))
	val, ok := r.fieldData[name]
	switch {
	case !ok && optional:
		return nil
	case !ok:
		return ErrMissingField
	case val == "":
		return nil
	}

	switch field.Interface().(type) {
	case string:
		if n, ok := val.(Number); ok {
			field.SetString(string(n))
			return nil
		}
		s, err := r.StringE(name)
		if err != nil {
			return err
		}
		field.SetString(s)
	case int, int8, int16, int32, int64:
		i, err := r.Int64E(name)
		if err != nil {
			return err
		}
		if field.OverflowInt(i) {
			return ErrOutOfRange
		}
		field.SetInt(i)
	case float32, float64:
		f, err := r.Float64E(name)
		if err != nil {
			return err
		}
		if field.OverflowFloat(f) {
			return ErrOutOfRange
		}
		field.SetFloat(f)
	case Number:
		n, err := r.NumberE(name)
		if err != nil {
			return err
		}
		field.SetString(string(n))
	case bool:
		field.SetBool(r.Bool(name))
	case time.Duration:
		// A distinct named type (underlying int64), so it is matched here
		// rather than by the integer case above.
		d, err := r.DurationE(name)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(d))
	case time.Time:
		t, err := r.TimeE(name)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(t))
	case *time.Time:
		t, err := r.TimeE(name)
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(&t))
	}
	return nil
}

// decodable reports whether Decode supports struct fields of type t: exactly
// the types decodeField's switch handles, so a defined type over one of them is
// not decodable.
func decodable(t reflect.Type) bool {
	switch reflect.Zero(t).Interface().(type) {
	case string, int, int8, int16, int32, int64, float32, float64, Number, bool,
		time.Duration, time.Time, *time.Time:
		return true
	}
	return false
}

// unsupportedTypeError reports an `fm`-tagged struct field type that Decode
// cannot populate. The fault is in the struct definition rather than the record
// — it is the same for every record — so it is reported rather than skipped.
func unsupportedTypeError(t reflect.Type) error {
	hint := ""
	if k := t.Kind(); k == reflect.Struct || (k == reflect.Pointer && t.Elem().Kind() == reflect.Struct) {
		hint = "; Decode is not recursive — call Decode on the nested struct itself"
	}
	return fmt.Errorf("unsupported type %s%s", t, hint)
}

// fieldError is one struct field Decode could not fill: the field, its `fm`
// tag, and why. Its message drops the wrapped error's package prefix, which
// decodeError carries once for all its fields.
type fieldError struct {
	field, tag string
	err        error
}

func (e *fieldError) Error() string {
	return fmt.Sprintf("%s (fm:%q): %s", e.field, e.tag, strings.TrimPrefix(e.err.Error(), "filemaker: "))
}

func (e *fieldError) Unwrap() error { return e.err }

// decodeError is the error Decode returns: every field it could not fill, in
// struct field order. Its message is a single line — the fields joined by "; "
// rather than errors.Join's newlines — so it stays whole in a log line or an
// error tracker's title, and it unwraps to each field's error so errors.Is
// matches any of their causes. It is unexported because the field list is for
// people reading the message; code branches on what it wraps.
type decodeError struct {
	errs []error
}

func (e *decodeError) Error() string {
	parts := make([]string, len(e.errs))
	for i, err := range e.errs {
		parts[i] = err.Error()
	}
	return "filemaker: decode: " + strings.Join(parts, "; ")
}

func (e *decodeError) Unwrap() []error { return e.errs }
