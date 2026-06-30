package filemaker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// SortOrder is the direction of a SortRule.
type SortOrder string

const (
	// SortAscending sorts the result in ascending order.
	SortAscending SortOrder = "ascend"
	// SortDescending sorts the result in descending order.
	SortDescending SortOrder = "descend"
)

// Criteria maps each field name to a find value expressed in FileMaker find
// syntax. The value is not a plain literal but a find expression, in which
// operators are characters embedded in the value: "==Mark" (exact match),
// "Mark*" (wildcard), ">10" (comparison), "1...10" (range), "=" (empty) or "*"
// (not empty). A field appears at most once per request, which the map enforces;
// to combine alternatives for the same field, use separate FindRequests.
//
// Because operator characters (among them @ * # ? ! = < > ") are interpreted
// rather than matched literally, a value built from arbitrary input may not
// match what you expect — an email address, for instance, whose "@" is the
// single-character wildcard — and such characters must be escaped with a
// backslash. Helper constructors that build correctly escaped expressions may be
// added later; for now the values are passed through verbatim.
type Criteria map[string]string

// FindRequest is a single find request passed to Find. Criteria maps each field
// name to a find value using FileMaker find syntax (see Criteria); the criteria
// are matched together (logical AND). Separate FindRequests are combined as
// alternatives (logical OR). Set Omit to exclude matching records.
type FindRequest struct {
	Criteria Criteria
	Omit     bool
}

// SortRule sorts the result by a field in the given order. It maps 1:1 to the
// Data API sort object, so JSON tags carry the wire names directly.
type SortRule struct {
	Field string    `json:"fieldName"`
	Order SortOrder `json:"sortOrder"`
}

// FindResponse is the result of a Find. Records is empty (non-nil) when no
// records match. DataInfo carries the host's record counts. Scripts holds the
// outcomes of any scripts run with the request (see WithScript).
type FindResponse struct {
	Records  []Record
	DataInfo DataInfo
	Scripts  ScriptOutcomes
}

// DataInfo mirrors the "dataInfo" object the host returns with a find result.
type DataInfo struct {
	Database         string `json:"database"`
	Layout           string `json:"layout"`
	Table            string `json:"table"`
	TotalRecordCount int    `json:"totalRecordCount"`
	FoundCount       int    `json:"foundCount"`
	ReturnedCount    int    `json:"returnedCount"`
}

// MarshalJSON renders the find request as a single flat object — the criteria
// with the "omit" directive folded in when Omit is set — matching the shape each
// element of the Data API "query" array expects, e.g.
//
//	{"Lastname":"==Johnson","omit":"true"}
//
// The criteria are copied into a fresh map so injecting "omit" never mutates the
// caller's Criteria.
func (r FindRequest) MarshalJSON() ([]byte, error) {
	m := make(map[string]string, len(r.Criteria)+1)
	for field, value := range r.Criteria {
		m[field] = value
	}
	if r.Omit {
		m["omit"] = "true"
	}
	return json.Marshal(m)
}

// Find runs the given find requests against the layout. The requests are
// combined as alternatives (logical OR); within a request the criteria are
// matched together (logical AND). A find that matches no records returns a
// FindResponse with an empty Records slice and a nil error.
//
// Pass WithSort, WithLimit, and WithOffset to order and page the result. Without
// WithLimit the host returns at most its default of 100 records, so pass
// WithLimit (with WithOffset to page) to retrieve more. (The Data API documents
// this default for the record-range endpoint; the find endpoint applies the same
// cap.)
//
// Returned related (portal) records are capped too: by default a portal returns
// at most the layout portal's configured row count, so a record can come back
// with fewer portal rows than exist. Use WithPortals to choose which portals are
// returned and WithPortalLimit/WithPortalOffset to page within one.
//
// Pass WithScript and friends to run scripts with the request; their outcomes are
// returned in the FindResponse's Scripts field.
func (c *Client) Find(ctx context.Context, layout string, requests []FindRequest, opts ...FindOption) (FindResponse, error) {
	if layout == "" {
		return FindResponse{}, errors.New("filemaker: no layout specified")
	}

	cfg, err := resolveFindConfig(opts)
	if err != nil {
		return FindResponse{}, err
	}

	body, err := marshalFindBody(requests, cfg)
	if err != nil {
		return FindResponse{}, err
	}

	var rb responseBody
	// ErrNoRecords means the find simply matched nothing; it is not an error to
	// the caller. Let it fall through with empty Data, which yields an empty
	// (non-nil) Records slice and still surfaces DataInfo and any script outcomes.
	if err := c.do(ctx, http.MethodPost, c.findURL(layout), body, &rb); err != nil && !errors.Is(err, ErrNoRecords) {
		return FindResponse{}, err
	}

	records := make([]Record, len(rb.Response.Data))
	for i, w := range rb.Response.Data {
		records[i] = Record{
			id:         w.ID,
			modID:      w.ModID,
			layout:     layout,
			fieldData:  w.FieldData,
			portalData: w.PortalData,
			loc:        c.location,
		}
	}
	return FindResponse{Records: records, DataInfo: rb.Response.DataInfo, Scripts: rb.scriptOutcomes()}, nil
}

// marshalFindBody renders the _find request body: the find requests under the
// "query" key (each marshals itself, folding in "omit"; see
// FindRequest.MarshalJSON), plus the read shaping the options set, each omitted
// when unset. A nil requests slice becomes an empty (non-nil) array so the body
// always carries the required "query" key.
//
// It is assembled as a map because the per-portal paging keys are dynamic
// ("offset.<portal>"/"limit.<portal>"); a fixed struct cannot express them.
func marshalFindBody(requests []FindRequest, cfg recordConfig) ([]byte, error) {
	if requests == nil {
		requests = []FindRequest{}
	}

	body := map[string]any{"query": requests}
	if len(cfg.sort) > 0 {
		body["sort"] = cfg.sort
	}
	if cfg.limit > 0 {
		body["limit"] = cfg.limit
	}
	if cfg.offset > 0 {
		body["offset"] = cfg.offset
	}
	if cfg.responseLayout != "" {
		body["layout.response"] = cfg.responseLayout
	}
	if len(cfg.portals) > 0 {
		body["portal"] = cfg.portals
	}
	for name, pr := range cfg.portalRanges {
		if pr.offset > 0 {
			body["offset."+name] = pr.offset
		}
		if pr.limit > 0 {
			body["limit."+name] = pr.limit
		}
	}
	for _, kv := range cfg.scriptParams() {
		body[kv[0]] = kv[1]
	}

	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("filemaker: failed to marshal find query: %w", err)
	}
	return out, nil
}
