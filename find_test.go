package filemaker

import (
	"fmt"
	"os"
	"testing"
)

func TestFind(t *testing.T) {
	conn, err := Connect(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer conn.Close()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", "=MJUK"),
		),
	)

	records, err := conn.PerformFind("fmi_cars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	fmt.Println("Records:", records)
}

func TestFindLimit(t *testing.T) {
	conn, err := Connect(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer conn.Close()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", ".."),
		),
	).SetLimit(1)

	records, err := conn.PerformFind("fmi_cars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	fmt.Println("Records:", records)
}

func TestFindOffset(t *testing.T) {
	conn, err := Connect(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer conn.Close()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", ".."),
		),
	).SetOffset(2)

	records, err := conn.PerformFind("fmi_cars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	fmt.Println("Records:", records)
}

func TestFindOmit(t *testing.T) {
	conn, err := Connect(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer conn.Close()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", ".."),
		),
		NewFindRequest(
			NewFindCriterion("D002_Aktiv", "=Aktiv"),
		).Omit(),
	)

	records, err := conn.PerformFind("fmi_cars", command)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	fmt.Println("Records:", records)
}

func TestFindNotFound(t *testing.T) {
	conn, err := Connect(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer conn.Close()

	var command = NewFindCommand(
		NewFindRequest(
			NewFindCriterion("D001_Registreringsnummer", "=foooooooooobaaaaaaaaaarrrrrrr"),
		),
	)

	records, err := conn.PerformFind("fmi_cars", command)
	if err != nil {
		switch err.(type) {
		case *ErrorNotFound:
			fmt.Println("Records not found!")
		default:
			fmt.Println("Error:", err.Error())
		}

		return
	}

	fmt.Println("Records:", records)
}
