package filemaker

import "encoding/json"

// SortOrder is the direction of a SortRule.
type SortOrder string

const (
	// SortAscending sorts the result in ascending order.
	SortAscending SortOrder = "ascend"
	// SortDescending sorts the result in descending order.
	SortDescending SortOrder = "descend"
)

// Criteria maps each field name to a find value expressed in FileMaker find
// syntax. The value is not a plain literal but a find expression, in which
// operators are characters embedded in the value: "==Mark" (exact match),
// "Mark*" (wildcard), ">10" (comparison), "1...10" (range), "=" (empty) or "*"
// (not empty). A field appears at most once per request, which the map enforces;
// to combine alternatives for the same field, use separate FindRequests.
//
// Because operator characters (among them @ * # ? ! = < > ") are interpreted
// rather than matched literally, a value built from arbitrary input may not
// match what you expect — an email address, for instance, whose "@" is the
// single-character wildcard — and such characters must be escaped with a
// backslash. Helper constructors that build correctly escaped expressions may be
// added later; for now the values are passed through verbatim.
type Criteria map[string]string

// FindRequest is a single find request passed to Find. Criteria maps each field
// name to a find value using FileMaker find syntax (see Criteria); the criteria
// are matched together (logical AND). Separate FindRequests are combined as
// alternatives (logical OR). Set Omit to exclude matching records.
type FindRequest struct {
	Criteria Criteria
	Omit     bool
}

// SortRule sorts the result by a field in the given order. It maps 1:1 to the
// Data API sort object, so JSON tags carry the wire names directly.
type SortRule struct {
	Field string    `json:"fieldName"`
	Order SortOrder `json:"sortOrder"`
}

// MarshalJSON renders the find request as a single flat object — the criteria
// with the "omit" directive folded in when Omit is set — matching the shape each
// element of the Data API "query" array expects, e.g.
//
//	{"Lastname":"==Johnson","omit":"true"}
//
// The criteria are copied into a fresh map so injecting "omit" never mutates the
// caller's Criteria.
func (r FindRequest) MarshalJSON() ([]byte, error) {
	m := make(map[string]string, len(r.Criteria)+1)
	for field, value := range r.Criteria {
		m[field] = value
	}
	if r.Omit {
		m["omit"] = "true"
	}
	return json.Marshal(m)
}
