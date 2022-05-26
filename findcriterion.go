package filemaker

//FindCriterion represents the findcriterion that builds up a findrequest
type FindCriterion struct {
	FieldName string
	Value     interface{}
}

//NewFindCriterion returns a new findcriterion
func NewFindCriterion(fieldName string, value interface{}) FindCriterion {
	return FindCriterion{
		fieldName,
		value,
	}
}
