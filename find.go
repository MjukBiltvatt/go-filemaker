package filemaker

//FindCriterion represents the findcriterion that builds up a findrequest
type FindCriterion struct {
	FieldName string
	Value     string
}

//FindRequest represents the findrequest that builds up a findcommand
type FindRequest map[string]string

//FindCommand represents the findcommand
// type FindCommand struct {
// 	Query          []interface{} `json:"query"`
// 	Limit          int           `json:"limit"`
// 	Offset         int           `json:"offset"`
// 	ResponseLayout string        `json:"layout.response"`
// }
type FindCommand map[string]interface{}

//NewFindCommand returns a findrequest
func NewFindCommand(requests ...interface{}) FindCommand {
	var query []interface{}

	for _, request := range requests {
		query = append(query, request)
	}

	var command = FindCommand{
		"query": query,
	}

	return command
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

//SetLimit sets the limit for the number of records returned by the findcommand
func (c FindCommand) SetLimit(limit int) {
	c["limit"] = limit
}

//SetOffset sets the offset for the records returned by the findcommand
func (c FindCommand) SetOffset(offset int) {
	c["offset"] = offset
}
