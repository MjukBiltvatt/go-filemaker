package filemaker

import (
	"fmt"
	"os"
	"testing"
)

func TestDelete(t *testing.T) {
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

	err = sess.Delete(record.Layout, record.ID)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}
}
