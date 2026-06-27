package filemaker

import (
	"encoding/json"
	"testing"
)

func TestQueryMarshalJSON(t *testing.T) {
	tests := []struct {
		name  string
		query Query
		want  string
	}{
		{
			name:  "empty query",
			query: Query{},
			want:  `{"query":[]}`,
		},
		{
			name: "single request with criteria",
			query: Query{
				Requests: []FindRequest{
					{Criteria: map[string]string{
						"Firstname": "Mark",
						"Age":       "*",
					}},
				},
			},
			want: `{"query":[{"Age":"*","Firstname":"Mark"}]}`,
		},
		{
			name: "omit request",
			query: Query{
				Requests: []FindRequest{
					{Criteria: map[string]string{"Lastname": "==Johnson"}, Omit: true},
				},
			},
			want: `{"query":[{"Lastname":"==Johnson","omit":"true"}]}`,
		},
		{
			name: "multiple requests",
			query: Query{
				Requests: []FindRequest{
					{Criteria: map[string]string{"Firstname": "Mark"}},
					{Criteria: map[string]string{"Lastname": "Johnson"}, Omit: true},
				},
			},
			want: `{"query":[{"Firstname":"Mark"},{"Lastname":"Johnson","omit":"true"}]}`,
		},
		{
			name: "limit, offset and sort",
			query: Query{
				Requests: []FindRequest{
					{Criteria: map[string]string{"Firstname": "Mark"}},
				},
				Sort: []SortRule{
					{Field: "Lastname", Order: SortAscending},
					{Field: "Age", Order: SortDescending},
				},
				Limit:  10,
				Offset: 5,
			},
			want: `{"query":[{"Firstname":"Mark"}],"sort":[{"fieldName":"Lastname","sortOrder":"ascend"},{"fieldName":"Age","sortOrder":"descend"}],"limit":10,"offset":5}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := json.Marshal(tt.query)
			if err != nil {
				t.Fatalf("Marshal returned error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got:  %s\nwant: %s", got, tt.want)
			}
		})
	}
}
