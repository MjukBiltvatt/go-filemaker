package errortypes

//ErrorNotFound is returned by Session.PerformFind() when no records match the request
type ErrorNotFound struct {
	message string
}

func (e *ErrorNotFound) Error() string {
	return e.message
}

//NewNotFound returns a new error of type ErrorNotFound
func NewNotFound() error {
	return &ErrorNotFound{"No records match the request."}
}
