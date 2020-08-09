package filemaker

import (
	"fmt"
	"os"
	"testing"
)

func TestCommit(t *testing.T) {
	conn, err := Connect(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer conn.Close()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", "=APPTEST1"),
		),
	).SetLimit(1)

	records, err := conn.PerformFind("fmi_appcars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	var record = records[0]

	record.SetField("D001_Registreringsnummer", "FOOBAR")
	err = conn.Commit(record)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}
}
