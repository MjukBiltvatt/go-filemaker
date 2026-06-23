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

// Query is a declarative find command. It is built as a plain struct literal and
// marshals to the FileMaker Data API _find request body. A Query with no
// Requests marshals to an empty query array.
type Query struct {
	Requests []Request
	Sort     []SortRule
	Limit    int // 0 = server default
	Offset   int // 0 = no offset
}

// Request is a single find request within a Query. Criteria maps each field
// name to a find value using FileMaker find syntax (e.g. "==exact", "*", "..");
// the criteria are matched together (logical AND). Separate Requests in a Query
// are combined as alternatives (logical OR). Set Omit to exclude matching
// records. A field can appear at most once per request, which the map enforces.
type Request struct {
	Criteria map[string]string
	Omit     bool
}

// SortRule sorts the result by a field in the given order. It maps 1:1 to the
// Data API sort object, so JSON tags carry the wire names directly.
type SortRule struct {
	Field string    `json:"fieldName"`
	Order SortOrder `json:"sortOrder"`
}

// MarshalJSON renders the Query into the Data API _find request body, e.g.
//
//	{"query":[{"Firstname":"Mark","omit":"true"}],"sort":[...],"limit":10}
func (q Query) MarshalJSON() ([]byte, error) {
	requests := make([]map[string]string, 0, len(q.Requests))
	for _, r := range q.Requests {
		// Copy into a fresh map so injecting "omit" never mutates the
		// caller's Criteria map.
		m := make(map[string]string, len(r.Criteria)+1)
		for field, value := range r.Criteria {
			m[field] = value
		}
		if r.Omit {
			m["omit"] = "true"
		}
		requests = append(requests, m)
	}

	wire := struct {
		Query  []map[string]string `json:"query"`
		Sort   []SortRule          `json:"sort,omitempty"`
		Limit  int                 `json:"limit,omitempty"`
		Offset int                 `json:"offset,omitempty"`
	}{
		Query:  requests,
		Sort:   q.Sort,
		Limit:  q.Limit,
		Offset: q.Offset,
	}

	return json.Marshal(wire)
}
