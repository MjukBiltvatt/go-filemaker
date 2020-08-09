package filemaker

import (
	"fmt"
	"os"
	"testing"
)

func TestFind1(t *testing.T) {
	conn, err := Connect(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer conn.Close()

	var command = NewFindCommand()

	var request1 = NewFindRequest()
	request1.AddFindCriterion("D001_Registreringsnummer", "=GHW915")
	request1.AddFindCriterion("Phone", "=0705445250")

	command.AddRequest(request1)

	records, err := conn.PerformFind("fmi_appcars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	fmt.Println("Records:", records)
}

func TestFind2(t *testing.T) {
	conn, err := Connect(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer conn.Close()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", "=GHW915"),
			NewFindCriterion("Phone", "=0705445250"),
		),
	)

	records, err := conn.PerformFind("fmi_appcars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	fmt.Println("Records:", records)
}
