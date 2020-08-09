package record

//NewEmpty returns a new empty record instance
func NewEmpty(layout string) Record {
	return Record{
		Layout:        layout,
		StagedChanges: make(map[string]interface{}),
	}
}
