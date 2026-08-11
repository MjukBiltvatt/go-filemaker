package filemaker

import (
	"errors"
	"strconv"
)

// responseBody mirrors the envelope every Data API call returns. The wire form
// encodes message codes as strings; check() converts them.
type responseBody struct {
	Response struct {
		Token       string       `json:"token"`
		RecordID    string       `json:"recordId"`
		ModID       string       `json:"modId"`
		DataInfo    DataInfo     `json:"dataInfo"`
		Data        []recordWire `json:"data"`
		ProductInfo ProductInfo  `json:"productInfo"`
		Databases   []Database   `json:"databases"`
		Scripts     []Script     `json:"scripts"`
		Layouts     []Layout     `json:"layouts"`

		// Script outcomes, one result/error pair per phase. The keys carry dots,
		// which Go json tags handle verbatim. Absent when no script ran.
		ScriptResult        string `json:"scriptResult"`
		ScriptError         string `json:"scriptError"`
		ScriptResultPre     string `json:"scriptResult.prerequest"`
		ScriptErrorPre      string `json:"scriptError.prerequest"`
		ScriptResultPresort string `json:"scriptResult.presort"`
		ScriptErrorPresort  string `json:"scriptError.presort"`

		// Layout metadata (single-layout endpoint).
		FieldMetaData  []FieldMetadata            `json:"fieldMetaData"`
		PortalMetaData map[string][]FieldMetadata `json:"portalMetaData"`
		ValueLists     []ValueList                `json:"valueLists"`
	} `json:"response"`
	Messages []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"messages"`
}

// recordWire is the wire shape of a "data" item. Record's field and portal maps
// are unexported (so records are immutable), which encoding/json cannot set, so
// the response decodes into this exported-field struct and Find builds Records
// from it.
type recordWire struct {
	ID         string                      `json:"recordId"`
	ModID      string                      `json:"modId"`
	FieldData  map[string]any              `json:"fieldData"`
	PortalData map[string][]map[string]any `json:"portalData"`
}

// scriptOutcomes assembles the per-phase script results the host reported. The
// zero value (all fields empty) means no scripts ran.
func (rb *responseBody) scriptOutcomes() ScriptOutcomes {
	return ScriptOutcomes{
		Script:     ScriptOutcome{Result: rb.Response.ScriptResult, Error: rb.Response.ScriptError},
		Prerequest: ScriptOutcome{Result: rb.Response.ScriptResultPre, Error: rb.Response.ScriptErrorPre},
		Presort:    ScriptOutcome{Result: rb.Response.ScriptResultPresort, Error: rb.Response.ScriptErrorPresort},
	}
}

// check inspects the host messages and returns an *APIError unless the primary
// message reports success (code 0). It guards against an empty messages array
// rather than indexing blindly.
func (rb *responseBody) check() error {
	if len(rb.Messages) == 0 {
		return errors.New("filemaker: response contained no messages")
	}

	if primary, err := strconv.Atoi(rb.Messages[0].Code); err == nil && primary == 0 {
		return nil
	}

	msgs := make([]Message, len(rb.Messages))
	for i, m := range rb.Messages {
		code, _ := strconv.Atoi(m.Code)
		msgs[i] = Message{Code: code, Text: m.Message}
	}
	return &APIError{Messages: msgs}
}
