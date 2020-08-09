package record

//SetField sets the value of a specified field in the given record
func (r Record) SetField(fieldName string, value interface{}) {
	r.StagedChanges[fieldName] = value
}
