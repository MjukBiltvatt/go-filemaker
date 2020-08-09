package filemaker

//FindCommand represents the findcommand
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

//SetLimit sets the limit for the number of records returned by the findcommand
func (c FindCommand) SetLimit(limit int) FindCommand {
	c["limit"] = limit
	return c
}

//SetOffset sets the offset for the records returned by the findcommand
func (c FindCommand) SetOffset(offset int) FindCommand {
	c["offset"] = offset
	return c
}

//Omit sets the findrequest to omit matching records
func (r FindRequest) Omit() FindRequest {
	r["omit"] = "true"
	return r
}
