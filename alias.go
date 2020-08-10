package filemaker

import (
	"github.com/jomla97/go-filemaker/pkg/connection"
	"github.com/jomla97/go-filemaker/pkg/errortypes"
	"github.com/jomla97/go-filemaker/pkg/record"
)

//Record represents the record object returned by performed findcommands
type Record = record.Record

//FieldChange represents the object returned by record.SetField()
type FieldChange = record.FieldChange

//Connection represents the connection object returned by filemaker.Connect()
type Connection = connection.Connection

//----------- Errors -----------

//ErrorNotFound is returned by Connection.PerformFind() when no records match the request
type ErrorNotFound = errortypes.ErrorNotFound
