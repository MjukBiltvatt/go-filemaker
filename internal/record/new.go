package record

//New returns a new empty record instance
func New(layout string, data interface{}) Record {
	return Record{
		ID:            data.(map[string]interface{})["recordId"].(string),
		Layout:        layout,
		FieldData:     data.(map[string]interface{})["fieldData"].(map[string]interface{}),
		StagedChanges: make(map[string]interface{}),
	}
}
