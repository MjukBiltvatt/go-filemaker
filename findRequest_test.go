package filemaker

import (
	"fmt"
	"testing"
)

func TestFindRequestOmit(t *testing.T) {
	var request = NewFindRequest(
		NewFindCriterion("criterion1field", "=data"),
	).Omit()

	fmt.Println("FindRequest:", request)
}

func TestFindRequestAddCriterion(t *testing.T) {
	var request = NewFindRequest(
		NewFindCriterion("criterion1field", "=data"),
	)

	request.AddCriterion(
		NewFindCriterion("criterion2field", "=data"),
	)

	fmt.Println("FindRequest:", request)
}
