package filemaker

import "encoding/json"

// FindRequest is a part required in a FindCommand and helps specify how a find is to be performed.
type FindRequest struct {
	//Criterions specifies the find criterion for each field.
	Criterions Fields
	//Omit specifies whether or not the fields matching this request should be omitted from the result.
	Omit bool
}

// NewFindRequest returns a new findrequest
func NewFindRequest(criterions Fields) FindRequest {
	return FindRequest{
		Criterions: criterions,
	}
}

// WithOmit returns a copy of the FindRequest with the specified omit value.
func (r FindRequest) WithOmit(omit bool) FindRequest {
	r.Omit = omit
	return r
}

// Set find criterion for the specified field
func (r *FindRequest) Set(fieldName string, value interface{}) {
	r.Criterions[fieldName] = value
}

// MarshalJSON marshals the find request into correct JSON
func (r *FindRequest) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{})

	for fieldName, value := range r.Criterions {
		m[fieldName] = value
	}

	if r.Omit {
		m["omit"] = "true"
	}

	return json.Marshal(m)
}
