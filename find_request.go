package filemaker

import "encoding/json"

// FindRequest is a component of a find request used to specify search criteria
type FindRequest struct {
	//The find criteria for fields
	Criteria Fields
	//Whether or not to omit matching records
	Omit bool
}

// NewFindRequest returns a new findrequest
func NewFindRequest(criteria Fields) FindRequest {
	return FindRequest{
		Criteria: criteria,
	}
}

// WithOmit returns a copy of the FindRequest with the specified omit value.
func (r FindRequest) WithOmit(omit bool) FindRequest {
	r.Omit = omit
	return r
}

// Set find criterion for the specified field
func (r *FindRequest) Set(fieldName string, value interface{}) {
	r.Criteria[fieldName] = value
}

// MarshalJSON marshals the find request into JSON
func (r *FindRequest) MarshalJSON() ([]byte, error) {
	m := make(map[string]interface{})

	for fieldName, value := range r.Criteria {
		m[fieldName] = value
	}

	if r.Omit {
		m["omit"] = "true"
	}

	return json.Marshal(m)
}
