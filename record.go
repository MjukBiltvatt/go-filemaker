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

// Record is a single record returned by a read operation (Find, Get). It is a
// plain, immutable value: it holds no reference back to the Client and has no
// methods that touch the host. All of its state is unexported and exposed
// through read-only accessors — ID, ModID, Layout, the field accessors (String,
// Int, …, Decode, Fields) and the portal accessors (Portal, PortalDataInfo) — so
// a returned record cannot be mutated. Writes are performed by passing field
// data to the Client's Create/Update methods, and portal rows with
// WithPortalData.
//
// Each typed field accessor reads one field as a Go type, by the same rules:
//
//   - A field the record does not have is ErrMissingField — usually a typo in
//     the name, or a field that is not on the layout (or the response layout).
//   - An empty field, which the host sends as "" whatever its type, reads as the
//     zero value without error. Use Get to tell it from a stored zero.
//   - A value the accessor cannot convert is the accessor's own error:
//     ErrNotString, ErrNotNumber, ErrNotInteger, ErrOutOfRange or
//     ErrUnknownFormat. The value returned with it is the zero value.
//
// Every error names the field and, for a record the host returned, the record
// and its layout, and it matches its cause with errors.Is. Decode applies the
// same rules to a whole struct, and PortalRow reads a portal's rows with the
// same accessors.
//
// A record remembers the host and database it was read from. The methods that
// take a Record (Get, Update, Delete, Duplicate, UploadToContainer) reject one
// read from a different database, where its layout and ID could name an
// unrelated record; any client connected to the same database accepts it.
type Record struct {
	fields // the field values, and the layout and ID that name the record

	modID string

	// host and database identify the file the record was read from, stamped by
	// the reading client. A layout and ID only name a record within one file, so
	// the endpoints that address a record through a Record refuse one read from
	// another file (see checkRecord).
	host     string
	database string

	portalData map[string][]map[string]any
	portalInfo map[string]PortalDataInfo
}

// checkRecord reports why rec cannot address a record through c, or nil if it
// can: it must carry an ID, and it must have been read from c's file. Another
// client's record would otherwise be sent with c's database and token, and its
// layout and ID would silently address whatever record holds them in c's file.
// Two clients on the same file (say, different accounts) can share records; the
// host is compared as an origin (see sameOrigin) and the database name
// case-insensitively, so spelling either differently is not a mismatch.
func (c *Client) checkRecord(rec Record) error {
	if rec.recordID == "" {
		return errors.New("filemaker: record has no ID; create or find it first")
	}
	if !sameOrigin(c.host, rec.host) || !strings.EqualFold(c.database, rec.database) {
		return fmt.Errorf("filemaker: record %q in layout %q was read from database %q on %s, not this client's database %q on %s",
			rec.recordID, rec.layout, rec.database, rec.host, c.database, c.host)
	}
	return nil
}

// ID returns the record's internal FileMaker record ID, assigned by the host.
func (r Record) ID() string { return r.recordID }

// ModID returns the record's modification ID, which the host changes on every
// edit. It is the basis for optimistic concurrency (see the Update IfUnchanged
// option).
func (r Record) ModID() string { return r.modID }

// Layout returns the layout the record was read through. It is the default
// target for the record-based writes (Update, Delete, UploadToContainer).
func (r Record) Layout() string { return r.layout }

// Portal returns the rows of the named portal, in the order the host sent them.
// A portal is named by its object name when it has one, otherwise by its
// table-occurrence name; the keys of PortalDataInfo list the record's portals.
// Each row reads its field values with the same accessors as the record. It is
// nil when the record has no such portal (WithPortals can leave one out) and
// empty for a portal with no rows.
func (r Record) Portal(name string) []PortalRow {
	rows, ok := r.portalData[name]
	if !ok {
		return nil
	}
	out := make([]PortalRow, len(rows))
	for i, row := range rows {
		// The host sends a row's record and mod IDs as plain "recordId" and
		// "modId" keys among its field values, as strings. They become the
		// row's ID and ModID rather than fields.
		data := cloneFields(row)
		id, _ := data["recordId"].(string)
		modID, _ := data["modId"].(string)
		delete(data, "recordId")
		delete(data, "modId")
		out[i] = PortalRow{
			fields: fields{
				origin: origin{layout: r.layout, recordID: r.recordID, portal: name, rowID: id},
				data:   data,
				loc:    r.loc,
			},
			modID: modID,
		}
	}
	return out
}

// PortalDataInfo returns the host's account of each portal on the record, keyed
// by the same portal names as Portal: the portal's object name when it has one,
// otherwise its table-occurrence name. It has an entry for exactly the portals
// the record carries — WithPortals omits a portal from both — including a portal
// with no rows. The result is a copy, and is nil when the host reports no portal
// information.
func (r Record) PortalDataInfo() map[string]PortalDataInfo {
	return maps.Clone(r.portalInfo)
}

// PortalRow is one related record in a portal, as returned by Record.Portal. It
// reads its field values with the same accessors, by the same rules, as a Record
// (String, Int, …, Decode, Fields), with each field keyed by its fully qualified
// name ("TableOccurrence::FieldName"). An error from any of them names the row,
// its portal, and the record and layout it was read through.
//
// A PortalRow is not a Record, so it cannot be passed to the methods that
// address a record (Update, Delete, …): its ID belongs to a record in the
// portal's table, not the layout's. Edit it through the parent record's Update
// with WithPortalData, as a PortalRowData carrying the row's ID (and its ModID to
// lock the edit).
type PortalRow struct {
	fields // the field values, and the portal, row and record that name them

	modID string
}

// ID returns the related record's internal FileMaker record ID.
func (p PortalRow) ID() string { return p.rowID }

// ModID returns the related record's modification ID.
func (p PortalRow) ModID() string { return p.modID }

// origin names the record or portal row a set of field values was read from,
// so an error reading them can say which one it concerns. The zero value names
// nothing: a Record built in the package rather than returned by the host.
type origin struct {
	layout   string // the layout the record was read through
	recordID string // the record, or for a portal row the record whose portal holds it
	portal   string // for a portal row, the portal's name
	rowID    string // for a portal row, the related record's ID
}

// identify adds o's identity to err (see recordErr and portalRowErr). It
// returns err unchanged when o names no record, and nil for a nil err.
func (o origin) identify(err error) error {
	switch {
	case err == nil || o.recordID == "":
		return err
	case o.portal != "":
		return portalRowErr(err, o.layout, o.recordID, o.portal, o.rowID)
	}
	return recordErr(err, o.layout, o.recordID)
}

// fields holds the field values of a record or portal row, and reads them. It
// is embedded in Record and PortalRow, so its accessors are theirs: a portal row
// reads exactly as a record does. The accessors' rules are documented on
// Record.
type fields struct {
	origin

	data map[string]any

	// loc is the time zone used to interpret date/timestamp fields. It is set
	// by the client from its WithLocation option; nil means UTC.
	loc *time.Location
}

// Fields returns a copy of the raw field values, keyed by field name, typed as
// FieldData describes (Number for numbers, string otherwise). The result is a
// copy — mutating it does not affect the record — and is nil when there is no
// field data. Use the typed accessors (String, Int, …) for individual fields.
func (f fields) Fields() FieldData {
	return cloneFields(f.data)
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

// location resolves the configured time zone, defaulting to UTC.
func (f fields) location() *time.Location {
	if f.loc != nil {
		return f.loc
	}
	return time.UTC
}

// Has reports whether the named field is present, even if empty.
func (f fields) Has(fieldName string) bool {
	_, ok := f.data[fieldName]
	return ok
}

// Get returns the raw value of a field, or nil if it is absent. Values are typed
// as FieldData describes: Number for numbers, string otherwise, and "" for an
// empty field of any type.
func (f fields) Get(fieldName string) any {
	return f.data[fieldName]
}

// String returns the value of a text field. A number field is ErrNotString;
// read it with Number, whose String method gives its exact text.
func (f fields) String(fieldName string) (string, error) {
	return read(f, fieldName, toString)
}

// StringSlice returns the value of a text field split on line breaks, treating
// it as a newline-separated list of values. Carriage returns, line feeds and
// CRLF pairs are all accepted as line breaks. Blank lines between values are
// preserved as empty strings, but a single trailing line break is treated as a
// terminator and does not produce a trailing empty element. An empty field
// yields a nil slice. A number field is ErrNotString.
func (f fields) StringSlice(fieldName string) ([]string, error) {
	return read(f, fieldName, toStringSlice)
}

// Number returns the value of a number field as a Number, with the host's exact
// digits. Use it for values the other numeric accessors cannot hold exactly:
// integers beyond int64 and decimals beyond float64's precision. Text, including
// text a user typed into a number field, is ErrNotNumber.
func (f fields) Number(fieldName string) (Number, error) {
	return read(f, fieldName, toNumber)
}

// Int returns the value of a number field as an int. It accepts only integer
// notation: a value with a fractional part or an exponent is ErrNotInteger
// rather than truncated (see Number.Int64), and one that does not fit an int is
// ErrOutOfRange. Text is ErrNotNumber.
func (f fields) Int(fieldName string) (int, error) {
	return read(f, fieldName, toInt)
}

// Int64 returns the value of a number field as an int64, exactly. It accepts
// only integer notation: a value with a fractional part or an exponent is
// ErrNotInteger rather than truncated (see Number.Int64), and one beyond int64
// is ErrOutOfRange. Text is ErrNotNumber.
func (f fields) Int64(fieldName string) (int64, error) {
	return read(f, fieldName, toInt64)
}

// Float64 returns the value of a number field as the nearest float64, which is
// exact only up to 15 significant digits; use Number to keep longer values
// exact. A magnitude beyond float64 is ErrOutOfRange, and text is ErrNotNumber.
func (f fields) Float64(fieldName string) (float64, error) {
	return read(f, fieldName, toFloat64)
}

// Bool returns the field value as a bool: non-empty text and numbers greater
// than 0 are true, and empty fields are false. FileMaker has no boolean type, so
// any present field reads as one; only an absent field is an error.
func (f fields) Bool(fieldName string) (bool, error) {
	return read(f, fieldName, toBool)
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

// TimeIn parses the value of a date, timestamp or time field as a time.Time in
// the given location. A value in none of the supported formats is
// ErrUnknownFormat. A time field holding 24 hours or more (an elapsed duration
// rather than a clock time) is out of the wall-clock range and will not parse
// here; read such a field with Duration instead.
func (f fields) TimeIn(fieldName string, loc *time.Location) (time.Time, error) {
	return read(f, fieldName, func(v any) (time.Time, error) { return toTime(v, loc) })
}

// Time parses the field value like TimeIn, in the configured location (set via
// the client's WithLocation option; UTC by default).
func (f fields) Time(fieldName string) (time.Time, error) {
	return f.TimeIn(fieldName, f.location())
}

// Duration parses the value of a time field, returned as a clock string
// ([-]HH:MM:SS), as a time.Duration. Unlike Time it represents the value as
// elapsed time, so it handles 24 hours or more and negative values. A value that
// is not such a string is ErrUnknownFormat.
func (f fields) Duration(fieldName string) (time.Duration, error) {
	return read(f, fieldName, toDuration)
}

// read implements the typed accessors: it looks up the field, applies the
// absent and empty rules documented on Record, and converts any other value
// with conv. A failure names the field and, through f's origin, the record or
// portal row.
func read[T any](f fields, fieldName string, conv func(any) (T, error)) (T, error) {
	var zero T
	val, ok := f.data[fieldName]
	switch {
	case !ok:
		return zero, f.fieldErr(fieldName, ErrMissingField)
	case val == "":
		return zero, nil
	}
	t, err := conv(val)
	if err != nil {
		return zero, f.fieldErr(fieldName, err)
	}
	return t, nil
}

// fieldErr is the error for a field that could not be read: err, naming the
// field and f's record or portal row.
func (f fields) fieldErr(fieldName string, err error) error {
	return f.identify(&readError{field: fieldName, err: err})
}

// readError is an accessor's failure to read a field: the field and why. Its
// message drops the wrapped error's package prefix and carries its own, which a
// record or portal row identity then keeps at the front.
type readError struct {
	field string
	err   error
}

func (e *readError) Error() string {
	return fmt.Sprintf("filemaker: field %q: %s", e.field, strings.TrimPrefix(e.err.Error(), "filemaker: "))
}

func (e *readError) Unwrap() error { return e.err }

// The to… functions convert a non-empty field value to a Go type, returning
// the bare cause when they cannot. They are shared by the typed accessors and
// Decode, which each add the field's identity to the error their own way.

func toString(v any) (string, error) {
	if s, ok := v.(string); ok {
		return s, nil
	}
	return "", ErrNotString
}

func toStringSlice(v any) ([]string, error) {
	s, err := toString(v)
	if err != nil || s == "" {
		return nil, err
	}
	// Normalize CRLF and lone CR to LF, then trim a single trailing line
	// break so a terminating newline does not yield an empty final element.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n"), nil
}

func toNumber(v any) (Number, error) {
	if n, ok := v.(Number); ok {
		return n, nil
	}
	return "", ErrNotNumber
}

func toInt64(v any) (int64, error) {
	n, err := toNumber(v)
	if err != nil {
		return 0, err
	}
	return n.Int64()
}

func toInt(v any) (int, error) {
	i, err := toInt64(v)
	if err != nil {
		return 0, err
	}
	if int64(int(i)) != i {
		return 0, ErrOutOfRange
	}
	return int(i), nil
}

func toFloat64(v any) (float64, error) {
	n, err := toNumber(v)
	if err != nil {
		return 0, err
	}
	return n.Float64()
}

func toBool(v any) (bool, error) {
	switch val := v.(type) {
	case string:
		return len(val) > 0, nil
	case Number:
		return val.positive(), nil
	}
	return false, nil
}

func toTime(v any, loc *time.Location) (time.Time, error) {
	s, _ := v.(string)
	for _, layout := range timeFormats {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, ErrUnknownFormat
}

func toDuration(v any) (time.Duration, error) {
	s, _ := v.(string)
	return parseFMDuration(s)
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

// Decode populates obj's fields from the record or portal row, matching each
// struct field's `fm` tag to a field name. obj must be a non-nil pointer to a
// struct.
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
// the same struct lingers. Values convert by the typed accessors' rules (see
// Record), with these additions:
//
//   - An empty field decodes to the zero value — nil for *time.Time — without
//     error, as it reads through the accessors.
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
// time.Time fields use the configured location; bool fields follow Bool, which
// accepts any value.
//
// Decode fills every field it can and reports all the fields it cannot in one
// error, in struct field order, alongside any struct-definition faults: an
// `fm`-tagged field whose type is not supported (which depends on the struct
// alone, so it surfaces on the first decode) or an unknown tag option. Each
// failure names the struct field and its tag, and the error matches each cause
// with errors.Is. For a record returned by the host, the error also names the
// record and its layout, and for a portal row the row and its portal too. A
// caller that prefers leniency can log the error and use the struct, which holds
// everything that decoded. Note that a defined type over a supported type (type
// Status string) is not itself supported.
//
// Supported field types: string, int, int8, int16, int32, int64, float32,
// float64, Number, bool, time.Duration, time.Time, *time.Time.
func (f fields) Decode(obj any) error {
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
		if err := f.decodeField(field, tag); err != nil {
			errs = append(errs, &fieldError{field: sf.Name, tag: tag, err: err})
		}
	}
	if len(errs) == 0 {
		return nil
	}
	return f.identify(&decodeError{errs: errs})
}

// decodeField sets one struct field from the field its `fm` tag names (see
// Decode for the rules), returning why it could not.
func (f fields) decodeField(field reflect.Value, tag string) error {
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
	val, ok := f.data[name]
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
		s, err := toString(val)
		if err != nil {
			return err
		}
		field.SetString(s)
	case int, int8, int16, int32, int64:
		i, err := toInt64(val)
		if err != nil {
			return err
		}
		if field.OverflowInt(i) {
			return ErrOutOfRange
		}
		field.SetInt(i)
	case float32, float64:
		fl, err := toFloat64(val)
		if err != nil {
			return err
		}
		if field.OverflowFloat(fl) {
			return ErrOutOfRange
		}
		field.SetFloat(fl)
	case Number:
		n, err := toNumber(val)
		if err != nil {
			return err
		}
		field.SetString(string(n))
	case bool:
		b, _ := toBool(val) // never fails: any present value is a bool
		field.SetBool(b)
	case time.Duration:
		// A distinct named type (underlying int64), so it is matched here
		// rather than by the integer case above.
		d, err := toDuration(val)
		if err != nil {
			return err
		}
		field.SetInt(int64(d))
	case time.Time:
		t, err := toTime(val, f.location())
		if err != nil {
			return err
		}
		field.Set(reflect.ValueOf(t))
	case *time.Time:
		t, err := toTime(val, f.location())
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
