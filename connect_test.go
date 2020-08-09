package filemaker

import (
	"fmt"
	"os"
	"testing"
)

func TestConnect(t *testing.T) {
	conn, err := Connect(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer conn.Close()
}
