package filemaker

import "testing"

func TestFindCommand(t *testing.T) {
	t.Run("NewFindCommand", func(t *testing.T) {
		command := NewFindCommand()
		if command == nil {
			t.Errorf("got: %v, expected: %v", command, "not nil")
		}
	})

	t.Run("Limit", func(t *testing.T) {
		command := NewFindCommand().Limit(10)
		if command["limit"] != 10 {
			t.Errorf("got: %v, expected: %v", command["limit"], 10)
		}
	})

	t.Run("Offset", func(t *testing.T) {
		command := NewFindCommand().Offset(10)
		if command["offset"] != 10 {
			t.Errorf("got: %v, expected: %v", command["offset"], 10)
		}
	})

	t.Run("Sort", func(t *testing.T) {
		command := NewFindCommand().Sort("fieldName", SortAscending)
		if command["sort"] == nil {
			t.Errorf("got: %v, expected: %v", command["sort"], "not nil")
		}
	})

	t.Run("AddRequest", func(t *testing.T) {
		command := NewFindCommand()
		request := NewFindRequest()
		command.AddRequest(request)
		if command["query"] == nil {
			t.Errorf("got: %v, expected: %v", command["query"], "not nil")
		}
	})
}
