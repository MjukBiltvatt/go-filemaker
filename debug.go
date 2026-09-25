package filemaker

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"regexp"
	"sync"
)

// authHeaderLine matches an Authorization header line in a request dump,
// capturing the leading whitespace-insensitive header name so its value can be
// masked. (?im) makes it case-insensitive and multiline so ^ anchors each line.
var authHeaderLine = regexp.MustCompile(`(?im)^(Authorization:).*$`)

// debugTransport is an http.RoundTripper that dumps every request and response
// passing through it to w before delegating to the wrapped transport. It is
// installed by WithDebug and sits at the transport layer, so it captures all of
// the client's traffic regardless of which method issued the request.
type debugTransport struct {
	rt http.RoundTripper // underlying transport, never nil
	w  io.Writer         // dump sink, never nil

	mu sync.Mutex // serializes dumps so concurrent requests do not interleave
}

// RoundTrip dumps the outgoing request, performs the round-trip, then dumps the
// response (or the transport error). The request and its response are written
// under a single lock so a request/response pair stays contiguous in the log
// even when many requests are in flight.
//
// Dumping never alters the outcome of the request: a failure to serialize the
// dump is itself logged but otherwise ignored, and the original response and
// error are always returned untouched.
func (d *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// DumpRequestOut drains and then restores req.Body (via GetBody), so it is
	// safe to dump the real request: the body is preserved for the round-trip
	// below. Redaction happens on the resulting bytes rather than by cloning the
	// request, which would share — and thus consume — the same body reader.
	reqDump, dumpErr := httputil.DumpRequestOut(req, true)
	if dumpErr == nil {
		reqDump = redactAuth(reqDump)
	}

	res, err := d.rt.RoundTrip(req)

	d.mu.Lock()
	defer d.mu.Unlock()

	if dumpErr != nil {
		fmt.Fprintf(d.w, "filemaker: failed to dump request: %v\n", dumpErr)
	} else {
		fmt.Fprintf(d.w, "filemaker debug: request\n%s\n", reqDump)
	}

	if err != nil {
		fmt.Fprintf(d.w, "filemaker debug: transport error: %v\n\n", err)
		return res, err
	}

	if resDump, derr := httputil.DumpResponse(res, true); derr != nil {
		fmt.Fprintf(d.w, "filemaker: failed to dump response: %v\n\n", derr)
	} else {
		fmt.Fprintf(d.w, "filemaker debug: response\n%s\n\n", resDump)
	}

	return res, err
}

// redactAuth masks the value of any Authorization header in a request dump, so
// neither the Basic-auth credentials sent on login nor the session Bearer token
// are ever written to the debug log.
func redactAuth(dump []byte) []byte {
	return authHeaderLine.ReplaceAll(dump, []byte("${1} REDACTED"))
}
