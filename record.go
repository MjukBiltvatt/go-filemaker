package filemaker

// Record is a single record returned by a read operation (Find/Get). It is a
// plain data carrier: it holds no reference back to the Client and has no
// methods that touch the host. Writes are performed by passing field data to
// the Client's Create/Update methods.
//
// FieldData and PortalData mirror the "fieldData" and "portalData" objects in
// the Data API response. ID and ModID correspond to "recordId" and "modId".
type Record struct {
	ID         string
	ModID      string
	Layout     string
	FieldData  map[string]any
	PortalData map[string][]map[string]any
}
