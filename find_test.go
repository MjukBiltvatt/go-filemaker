package filemaker

import (
	"testing"
)

func TestMarshalFindBody(t *testing.T) {
	tests := []struct {
		name     string
		requests []FindRequest
		cfg      recordConfig
		want     string
	}{
		{
			name: "empty query",
			want: `{"query":[]}`,
		},
		{
			name: "single request with criteria",
			requests: []FindRequest{
				{Criteria: map[string]string{
					"Firstname": "Mark",
					"Age":       "*",
				}},
			},
			want: `{"query":[{"Age":"*","Firstname":"Mark"}]}`,
		},
		{
			name: "omit request",
			requests: []FindRequest{
				{Criteria: map[string]string{"Lastname": "==Johnson"}, Omit: true},
			},
			want: `{"query":[{"Lastname":"==Johnson","omit":"true"}]}`,
		},
		{
			name: "multiple requests",
			requests: []FindRequest{
				{Criteria: map[string]string{"Firstname": "Mark"}},
				{Criteria: map[string]string{"Lastname": "Johnson"}, Omit: true},
			},
			want: `{"query":[{"Firstname":"Mark"},{"Lastname":"Johnson","omit":"true"}]}`,
		},
		{
			name: "limit, offset and sort",
			requests: []FindRequest{
				{Criteria: map[string]string{"Firstname": "Mark"}},
			},
			cfg: recordConfig{
				sort: []SortRule{
					{Field: "Lastname", Order: SortAscending},
					{Field: "Age", Order: SortDescending},
				},
				limit:  10,
				offset: 5,
			},
			want: `{"limit":10,"offset":5,"query":[{"Firstname":"Mark"}],"sort":[{"fieldName":"Lastname","sortOrder":"ascend"},{"fieldName":"Age","sortOrder":"descend"}]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := marshalFindBody(tt.requests, tt.cfg)
			if err != nil {
				t.Fatalf("marshalFindBody returned error: %v", err)
			}
			if string(got) != tt.want {
				t.Errorf("got:  %s\nwant: %s", got, tt.want)
			}
		})
	}
}
