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
