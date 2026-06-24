package filemaker

import (
	"errors"
	"fmt"
	"strings"
)

// codeError is a sentinel error that carries the FileMaker host code it stands
// for, so *APIError.Is can match it generically without a code→sentinel lookup.
type codeError struct {
	code int
	text string
}

func (e *codeError) Error() string { return e.text }

var (
	ErrNotNumber     = errors.New("value is not a number")
	ErrNotString     = errors.New("value is not a string")
	ErrUnknownFormat = errors.New("unknown format")

	// Host-code sentinels. The library exposes a sentinel only for a host code it
	// attaches control-flow meaning to — currently the three below, each of which
	// the client itself interprets (Find swallows 401, the reauth path keys off
	// 952, Update surfaces 306). FileMaker defines hundreds of other codes, but
	// the vast majority are developer-time faults (missing layout, bad calc,
	// malformed request) that callers fix rather than branch on, so mirroring the
	// host's full error table here would be churn without value. To branch on any
	// other code, unpack the error instead:
	//
	//	var apiErr *filemaker.APIError
	//	if errors.As(err, &apiErr) { switch apiErr.Code() { … } }
	//
	// Adding a sentinel later is a one-line, non-breaking change — a new codeError
	// here, no APIError.Is change — so the bar for a new one is a concrete,
	// recurring runtime branch, not completeness.

	// ErrRecordModified is returned (wrapped) by Update when a WithModID check
	// fails because the record changed since the mod ID was read (optimistic-lock
	// conflict). It corresponds to host code 306 and is also matched by errors.Is
	// against any *APIError carrying that code.
	ErrRecordModified error = &codeError{306, "filemaker: record modified since mod ID was read"}

	// ErrNoRecords matches an *APIError whose host code is 401 ("no records match
	// the request"). Find translates 401 into an empty FindResponse with a nil
	// error, so this sentinel surfaces only from operations that treat "no
	// records" as a genuine failure. Test for it with errors.Is.
	ErrNoRecords error = &codeError{401, "filemaker: no records match the request"}

	// ErrInvalidToken matches an *APIError whose host code is 952 (invalid or
	// expired session token). When the client is built with
	// WithReauthOnInvalidToken it re-authenticates and retries automatically, so
	// this surfaces only when that option is unset or the retry itself fails.
	// Test for it with errors.Is.
	ErrInvalidToken error = &codeError{952, "filemaker: invalid or expired session token"}
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

// Is reports whether the error matches a sentinel for a common host code, so
// callers can branch with errors.Is(err, ErrInvalidToken) and the like without
// unpacking *APIError or hard-coding numeric codes. It matches when any message
// in the response carries the corresponding code.
func (e *APIError) Is(target error) bool {
	ce, ok := target.(*codeError)
	if !ok {
		return false
	}
	for _, m := range e.Messages {
		if m.Code == ce.code {
			return true
		}
	}
	return false
}
