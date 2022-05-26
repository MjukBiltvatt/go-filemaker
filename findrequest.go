package filemaker

//FindRequest represents the findrequest that builds up a findcommand
type FindRequest map[string]interface{}

//NewFindRequest returns a new findrequest
func NewFindRequest(criterions ...FindCriterion) FindRequest {
	var request = make(FindRequest)

	for _, criterion := range criterions {
		request[criterion.FieldName] = criterion.Value
	}

	return request
}

//Omit sets the findrequest to omit matching records
func (r FindRequest) Omit() FindRequest {
	r["omit"] = "true"
	return r
}

//AddCriterion appends a specified FindCriterion to the FindRequest
func (r *FindRequest) AddCriterion(criterion FindCriterion) {
	(*r)[criterion.FieldName] = criterion.Value
}
