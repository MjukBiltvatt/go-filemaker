package filemaker

import (
	"fmt"
	"testing"
)

func TestFindCommandLimit(t *testing.T) {
	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("fieldname", "=data"),
		),
	).SetLimit(1)

	fmt.Println("FindCommand:", command)
}

func TestFindCommandOffset(t *testing.T) {
	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("fieldname", "=data"),
		),
	).SetOffset(1)

	fmt.Println("FindCommand:", command)
}

func TestFindCommandAddRequest(t *testing.T) {
	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("request1field", "=data"),
		),
	)
	command.AddRequest(
		NewFindRequest(
			NewFindCriterion("request2field", "=data"),
		),
	)

	fmt.Println("FindCommand:", command)
}
