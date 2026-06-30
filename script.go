package filemaker

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

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
