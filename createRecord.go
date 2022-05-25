package filemaker

import "github.com/MjukBiltvatt/go-filemaker/pkg/record"

//CreateRecord creates a new empty local record that still needs to be committed to the server
func CreateRecord(layout string) record.Record {
	return record.NewEmpty(layout)
}
