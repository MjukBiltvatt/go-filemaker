package record

//FieldChange interface for changing data in field
type FieldChange struct {
	FieldName string
	Value     interface{}
}

//Record interface for some magic with methods
type Record struct {
	ID            string
	Layout        string
	StagedChanges map[string]interface{}
	fieldData     map[string]interface{}
}
