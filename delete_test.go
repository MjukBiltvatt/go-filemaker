package filemaker

import (
	"fmt"
	"os"
	"testing"
)

func TestDelete(t *testing.T) {
	conn, err := Connect(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer conn.Close()

	var record = CreateRecord("fmi_appcars")
	record.SetField("D001_Registreringsnummer", "FOOBAR")

	err = conn.Commit(&record)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	err = conn.Delete(record.Layout, record.ID)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}
}
