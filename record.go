package filemaker

// FieldData holds a record's fields keyed by field name. When read back from the
// host, FileMaker number fields decode as float64 and text, date and timestamp
// fields as string.
type FieldData map[string]any

// Record is a single record returned by a read operation (Find). It is a
// plain data carrier: it holds no reference back to the Client and has no
// methods that touch the host. Writes are performed by passing field data to
// the Client's Create/Update methods.
//
// The JSON tags map each field to the Data API "data" item shape. Layout is not
// part of that shape (it comes from the request), so it is tagged "-" and set
// by the client after decoding.
type Record struct {
	ID         string                      `json:"recordId"`
	ModID      string                      `json:"modId"`
	Layout     string                      `json:"-"`
	FieldData  FieldData                   `json:"fieldData"`
	PortalData map[string][]map[string]any `json:"portalData"`
}
