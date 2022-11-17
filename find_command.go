package filemaker

// FindCommand specifies how a find should be performed and consists of find requests.
type FindCommand struct {
	//Requests specifies the find requests for the command.
	Requests []FindRequest `json:"query"`
	//Limit the number of records returned in the result.
	Limit int `json:"limit"`
	//Offset the records returned in the result.
	Offset int `json:"offset"`
}

// NewFindCommand returns a new FindCommand with the specified find requests
func NewFindCommand(requests ...FindRequest) (c FindCommand) {
	for _, request := range requests {
		c.AddRequest(request)
	}
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
