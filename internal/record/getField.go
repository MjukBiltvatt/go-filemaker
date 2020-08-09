package record

//GetField gets the value of a field in the given record
func (r Record) GetField(fieldName string) interface{} {
	return r.fieldData[fieldName]
}
