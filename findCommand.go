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

//Limit sets the limit for the number of records returned by the findcommand
func (c FindCommand) Limit(limit int) FindCommand {
	c["limit"] = limit
	return c
}

//Offset sets the offset for the records returned by the findcommand
func (c FindCommand) Offset(offset int) FindCommand {
	c["offset"] = offset
	return c
}

//AddRequest appends a specified FindRequest to the FindCommand
func (c *FindCommand) AddRequest(request FindRequest) {
	if query, ok := (*c)["query"]; ok {
		(*c)["query"] = append(query.([]interface{}), request)
	} else {
		var query []interface{}
		(*c)["query"] = append(query, request)
	}
}
