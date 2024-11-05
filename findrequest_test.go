package filemaker

import (
	"testing"
)

func TestFindRequest(t *testing.T) {
	t.Run("NewFindRequest", func(t *testing.T) {
		request := NewFindRequest()
		if request == nil {
			t.Errorf("got: %v, expected: %v", request, "not nil")
		}
	})

	t.Run("OmitFalse", func(t *testing.T) {
		request := NewFindRequest()
		if request["omit"] != "false" && request["omit"] != nil {
			t.Errorf("got: %v, expected: %v", request["omit"], "nil")
		}
	})

	t.Run("OmitTrue", func(t *testing.T) {
		request := NewFindRequest().Omit()
		if request["omit"] != "true" {
			t.Errorf("got: %v, expected: %v", request["omit"], "true")
		}
	})

	t.Run("AddCriterion", func(t *testing.T) {
		request := NewFindRequest()
		criterion := FindCriterion{FieldName: "Foo", Value: "Bar"}
		request.AddCriterion(criterion)
		if request["Foo"] != "Bar" {
			t.Errorf("got: %v, expected: %v", request["Foo"], "Bar")
		}
	})
}
