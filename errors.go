package filemaker

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrNotNumber     = errors.New("value is not a number")
	ErrNotString     = errors.New("value is not a string")
	ErrUnknownFormat = errors.New("unknown format")
)

// Message is a single status message returned by the FileMaker host.
type Message struct {
	Code int
	Text string
}

// APIError is returned when the FileMaker host reports a non-OK result. It
// carries every message from the response (the API models messages as an
// array, though a single message is the norm).
type APIError struct {
	Messages []Message
}

// Error joins all messages from the host response.
func (e *APIError) Error() string {
	if len(e.Messages) == 0 {
		return "filemaker: unknown host error"
	}
	parts := make([]string, len(e.Messages))
	for i, m := range e.Messages {
		parts[i] = fmt.Sprintf("%s (%d)", m.Text, m.Code)
	}
	return "filemaker: " + strings.Join(parts, "; ")
}

// Code returns the code of the first message, which the Data API uses as the
// primary result of a call. It returns 0 when there are no messages.
func (e *APIError) Code() int {
	if len(e.Messages) == 0 {
		return 0
	}
	return e.Messages[0].Code
}
