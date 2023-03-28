package filemaker

import "encoding/json"

// FindCommand specifies how a find should be performed and consists of find requests.
type FindCommand struct {
	//A list of find requests
	Requests []FindRequest `json:"query"`
	//Limit the number of records returned in the result
	Limit int `json:"limit"`
	//Offset the records returned in the result
	Offset int `json:"offset"`
}

// NewFindCommand returns a new FindCommand with the specified find requests
func NewFindCommand(requests ...FindRequest) (c FindCommand) {
	c.Requests = append(c.Requests, requests...)
	return c
}

// WithLimit returns a copy of the FindCommand with the specified limit.
func (c FindCommand) WithLimit(limit int) FindCommand {
	c.Limit = limit
	return c
}

// WithOffset returns a copy of the FindCommand with the specified offset.
func (c FindCommand) WithOffset(offset int) FindCommand {
	c.Offset = offset
	return c
}

// AddRequest appends a specified FindRequest to the FindCommand
func (c *FindCommand) AddRequest(request FindRequest) {
	c.Requests = append(c.Requests, request)
}

// MarshalJSON marshals the find command into JSON
func (c *FindCommand) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{})
	m["query"] = c.Requests

	if c.Limit > 0 {
		m["limit"] = c.Limit
	}
	if c.Offset > 0 {
		m["offset"] = c.Offset
	}

	return json.Marshal(m)
}
