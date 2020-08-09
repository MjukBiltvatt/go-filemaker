package main

import (
	"fmt"
	"os"

	"github.com/jomla97/go-fm-rest/connection"
)

func main() {
	test()
}

func test() {
	conn, err := Connect(os.Getenv("HOST"), os.Getenv("DATABASE"), os.Getenv("USERNAME"), os.Getenv("PASSWORD"))
	if err != nil {
		fmt.Println("Error: ", err.Error())
		return
	}
	defer conn.Close()

	fmt.Println("Token: ", conn.Token)
}

//Connect starts a database session
func Connect(host string, database string, username string, password string) (*connection.Connection, error) {
	return connection.New(host, database, username, password)
}
