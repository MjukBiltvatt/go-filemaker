package filemaker

import "errors"

var (
	ErrNotNumber     = errors.New("value is not a number")
	ErrNotString     = errors.New("value is not a string")
	ErrUnknownFormat = errors.New("unknown format")
)
