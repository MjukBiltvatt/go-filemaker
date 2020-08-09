package main

import (
	"fmt"
	"os"

	"github.com/jomla97/go-fm-rest/internal/connection"
)

func main() {
	test()
}

func test() {
	conn, err := Connect(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error:", err.Error())
		return
	}
	defer conn.Close()

	var findRequest = struct {
		Query []interface{} `json:"query"`
	}{
		Query: []interface{}{
			struct {
				Reg string `json:"D001_Registreringsnummer"`
			}{
				Reg: "=GHW915",
			},
		},
	}

	records, err := conn.Find("fmi_appcars", findRequest)
	if err != nil {
		fmt.Println("Error:", err.Error())
	}

	fmt.Println("Records:", records)
}

//Connect starts a database session
func Connect(host string, database string, username string, password string) (*connection.Connection, error) {
	return connection.New(host, database, username, password)
}
