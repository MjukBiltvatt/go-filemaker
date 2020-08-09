package filemaker

//FindCriterion represents the findcriterion that builds up a findrequest
type FindCriterion struct {
	FieldName string
	Value     string
}

//FindRequest represents the findrequest that builds up a findcommand
type FindRequest map[string]string

//FindCommand represents the findcommand
type FindCommand struct {
	Query []interface{} `json:"query"`
}

//NewFindCommand returns a findrequest
func NewFindCommand(requests ...interface{}) FindCommand {
	var query []interface{}

	for _, request := range requests {
		query = append(query, request)
	}

	return FindCommand{
		query,
	}
}

//NewFindRequest returns a new findrequest
func NewFindRequest(criterions ...FindCriterion) FindRequest {
	var request = make(FindRequest)

	for _, criterion := range criterions {
		request[criterion.FieldName] = criterion.Value
	}

	return request
}

//NewFindCriterion returns a new findcriterion
func NewFindCriterion(fieldName string, value string) FindCriterion {
	return FindCriterion{
		fieldName,
		value,
	}
}

//AddFindCriterion adds a findcriterion to a findrequest
func (r FindRequest) AddFindCriterion(fieldName string, value string) {
	r[fieldName] = value
}

//AddRequest adds a findrequest to a findcommand
func (c FindCommand) AddRequest(request FindRequest) {
	c.Query = append(c.Query, request)
}
