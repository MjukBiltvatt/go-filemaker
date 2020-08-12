package filemaker

import (
	"fmt"
	"os"
	"testing"
)

func TestFind(t *testing.T) {
	sess, err := New(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer sess.Destroy()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", "=MJUK"),
		),
	)

	records, err := sess.PerformFind("fmi_cars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	fmt.Println("Records:", records)
}

func TestFindLimit(t *testing.T) {
	sess, err := New(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer sess.Destroy()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", ".."),
		),
	).SetLimit(1)

	records, err := sess.PerformFind("fmi_cars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	fmt.Println("Records:", records)
}

func TestFindOffset(t *testing.T) {
	sess, err := New(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer sess.Destroy()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", ".."),
		),
	).SetOffset(2)

	records, err := sess.PerformFind("fmi_cars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	fmt.Println("Records:", records)
}

func TestFindOmit(t *testing.T) {
	sess, err := New(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer sess.Destroy()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", ".."),
		),
		NewFindRequest(
			NewFindCriterion("D002_Aktiv", "=Aktiv"),
		).Omit(),
	)

	records, err := sess.PerformFind("fmi_cars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	fmt.Println("Records:", records)
}

func TestFindNotFound(t *testing.T) {
	sess, err := New(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer sess.Destroy()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", "=foooooooooobaaaaaaaaaarrrrrrr"),
		),
	)

	records, err := sess.PerformFind("fmi_cars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}

	if len(records) > 0 {
		fmt.Println("Records:", records)
	} else {
		fmt.Println("No records found")
	}
}
