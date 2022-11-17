package filemaker

// Fields is a component of a FindRequest and specifies the find criterions for each field in the find request.
type Fields map[string]interface{}

// NewFields makes a new Fields map
func NewFields() Fields {
	return make(Fields)
}
