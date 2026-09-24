package filemaker

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// codeError is a sentinel error that carries the FileMaker host code it stands
// for, so *APIError.Is can match it generically without a code→sentinel lookup.
type codeError struct {
	code int
	text string
}

func (e *codeError) Error() string { return e.text }

var (
	ErrNotNumber     = errors.New("filemaker: value is not a number")
	ErrNotString     = errors.New("filemaker: value is not a string")
	ErrUnknownFormat = errors.New("filemaker: unknown format")

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
//
// The first message is the primary result of the call, and it alone decides the
// error's meaning: it is what [APIError.Code] reports, what determines whether a
// response counts as a failure at all, and what [APIError.Is] matches sentinels
// against. Any further messages are carried for diagnostics only — never matched
// — so an incidental 401 or 952 in a secondary message cannot make an unrelated
// failure look like a no-records or expired-token one.
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
// unpacking *APIError or hard-coding numeric codes. It matches on the primary
// code (see Code), the same message the response check treats as the result of
// the call, so a sentinel never matches on an incidental secondary message.
func (e *APIError) Is(target error) bool {
	ce, ok := target.(*codeError)
	if !ok {
		return false
	}
	return e.Code() == ce.code
}

// HTTPError is returned when an exchange never reached Data API semantics: a
// proxy or gateway answered instead of the host, the body could not be decoded
// as a Data API response, or the container streaming endpoint refused the
// request. It is the counterpart to *[APIError], and the two divide cleanly:
// an *APIError means FileMaker received the request and reported a result,
// while an HTTPError means the exchange did not get that far. For a write that
// distinction is the difference between "the host rejected this" and "this may
// or may not have been applied".
//
// StatusCode is the HTTP status of the response, as sent by whoever answered: a
// gateway, a misrouted proxy, or FileMaker Server's own web tier. It need not
// signal failure — a proxy routed to the wrong backend can answer 200 — so read
// it as a diagnostic rather than a retry policy. Retrying is safe for reads;
// retrying a write risks applying it twice, since the first attempt may have
// reached the host.
//
// Err is the decode failure when there was one, and nil otherwise; it is
// reachable with errors.As through [HTTPError.Unwrap].
type HTTPError struct {
	StatusCode int
	Err        error

	// snippet is the start of the response body, for diagnostics. A gateway
	// names the actual fault in its body ("Error 1016: Origin DNS error") and
	// nowhere else, so it is folded into the message rather than dropped. It is
	// unexported because it is material for a log line, not something to branch
	// on.
	snippet string
}

// Error reports what went wrong with the status as context and, when one was
// captured, the start of the body. The status is context rather than the
// subject because it need not be a failure: a proxy routed to the wrong backend
// can answer 200 with a body that is simply not a Data API response.
func (e *HTTPError) Error() string {
	status := strconv.Itoa(e.StatusCode)
	if text := http.StatusText(e.StatusCode); text != "" {
		// Blank for the codes a gateway is most likely to invent — Cloudflare's
		// 520-527 and 530 are unregistered — which is why the body carries the
		// diagnostic weight here.
		status += " " + text
	}

	msg := fmt.Sprintf("filemaker: unexpected response (HTTP %s)", status)
	if e.Err != nil {
		msg = fmt.Sprintf("filemaker: failed to decode response (HTTP %s)", status)
	}

	switch {
	case e.snippet != "":
		// The snippet supersedes Err: a decode failure reports the first byte it
		// choked on, which is the first byte of the snippet being printed next.
		return msg + ": " + e.snippet
	case e.Err != nil:
		return msg + ": " + e.Err.Error()
	}
	return msg
}

// Unwrap returns the decode failure, if any, so callers can match the
// underlying error (a *json.SyntaxError, say) with errors.As.
func (e *HTTPError) Unwrap() error { return e.Err }

// bodySnippet renders the start of a response body for an error message:
// whitespace collapsed to single spaces so an HTML error page stays on one
// line, and truncated on a rune boundary so the result is always valid UTF-8.
// It returns "" for a body that is empty or entirely whitespace.
//
// Only the first maxBytes are examined. A body reaching here can be a whole
// response that failed to decode near its end, and copying megabytes to render
// a 200-rune diagnostic would be a poor trade.
func bodySnippet(body []byte) string {
	const (
		maxBytes = 4 << 10
		maxRunes = 200
	)

	if len(body) > maxBytes {
		body = body[:maxBytes]
		// The byte cut can land inside a rune, and nothing downstream would drop
		// the remainder: an invalid sequence decodes as RuneError one byte at a
		// time, which is not whitespace and so survives the collapse below. Trim
		// it here instead. A real U+FFFD in the body decodes with its full width,
		// so only an incomplete tail is removed.
		for len(body) > 0 {
			if r, size := utf8.DecodeLastRune(body); r == utf8.RuneError && size <= 1 {
				body = body[:len(body)-1]
				continue
			}
			break
		}
	}

	collapsed := strings.Join(strings.Fields(string(body)), " ")
	if collapsed == "" {
		return ""
	}

	runes := []rune(collapsed)
	if len(runes) <= maxRunes {
		return collapsed
	}
	return string(runes[:maxRunes]) + "…"
}
