package filemaker

//FindCriterion represents the findcriterion that builds up a findrequest
type FindCriterion struct {
	FieldName string
	Value     string
}

//NewFindCriterion returns a new findcriterion
func NewFindCriterion(fieldName string, value string) FindCriterion {
	return FindCriterion{
		fieldName,
		value,
	}
}
