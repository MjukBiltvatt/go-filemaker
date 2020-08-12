package filemaker

import (
	"fmt"
	"os"
	"testing"
)

func TestCommitEdit(t *testing.T) {
	sess, err := New(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer sess.Destroy()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", "=APPTEST1"),
		),
	).SetLimit(1)

	records, err := sess.PerformFind("fmi_appcars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	var record = records[0]
	record.SetField("D001_Registreringsnummer", "FOOBAR")

	err = sess.Commit(&record)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}
}

func TestCommitCreate(t *testing.T) {
	sess, err := New(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer sess.Destroy()

	var record = CreateRecord("fmi_appcars")
	record.SetField("D001_Registreringsnummer", "FOOBAR")

	err = sess.Commit(&record)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}
}
