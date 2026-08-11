package filemaker

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

// ScriptOutcome is what one script phase produced: the value the script returned
// via Exit Script, and a FileMaker error code ("0" on success). Both are empty
// when no script ran for that phase.
//
// Use Ran/OK rather than inspecting Result: the host returns an Error code
// whenever a script runs and omits it otherwise, so Error is the reliable signal
// of whether a script ran. An empty Result is ambiguous on its own (a script may
// return an empty value).
type ScriptOutcome struct {
	Result string
	Error  string
}

// Ran reports whether a script ran for this phase. The host returns an Error
// code ("0" on success) whenever a script runs and omits it otherwise, so an
// empty Error means no script ran.
func (o ScriptOutcome) Ran() bool { return o.Error != "" }

// OK reports whether a script ran and completed without error.
func (o ScriptOutcome) OK() bool { return o.Error == "0" }

// ScriptOutcomes groups the outcomes of the scripts run with a request, one per
// phase, matching the WithScript / WithPrerequestScript / WithPresortScript
// options.
type ScriptOutcomes struct {
	Script     ScriptOutcome // WithScript — runs after the action
	Prerequest ScriptOutcome // WithPrerequestScript
	Presort    ScriptOutcome // WithPresortScript
}

// RunScriptResponse is the result of a RunScript.
type RunScriptResponse struct {
	Script ScriptOutcome
}

// RunScript runs the named script in the context of layout. param is the script
// parameter (pass "" for none). The outcome — the value the script returned via
// Exit Script and the FileMaker error code — is returned in the RunScriptResponse.
//
// A script that runs but ends with a non-zero error code is not a request
// failure: RunScript returns nil and the error is reported in Script.Error. A
// request failure (e.g. the script does not exist — FileMaker error 104) is
// returned as an *APIError.
func (c *Client) RunScript(ctx context.Context, layout, name, param string) (RunScriptResponse, error) {
	switch {
	case layout == "":
		return RunScriptResponse{}, errors.New("filemaker: no layout specified")
	case name == "":
		return RunScriptResponse{}, errors.New("filemaker: no script name specified")
	}

	u := c.scriptURL(layout, name)
	if param != "" {
		q := url.Values{}
		q.Set("script.param", param)
		u += "?" + q.Encode()
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodGet, u, nil, &rb); err != nil {
		return RunScriptResponse{}, err
	}
	return RunScriptResponse{
		Script: ScriptOutcome{
			Result: rb.Response.ScriptResult,
			Error:  rb.Response.ScriptError,
		},
	}, nil
}
