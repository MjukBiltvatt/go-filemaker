package filemaker

import (
	"github.com/jomla97/go-filemaker/internal/connection"
	"github.com/jomla97/go-filemaker/internal/record"
)

//Record represents the record object returned by performed findcommands
type Record record.Record

//FieldChange represents the object returned by record.SetField()
type FieldChange record.FieldChange

//Connection represents the connection object returned by filemaker.Connect()
type Connection connection.Connection
