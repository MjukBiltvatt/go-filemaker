package filemaker

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

// GetResponse is the result of a Get or GetByID.
type GetResponse struct {
	Record  Record
	Scripts ScriptOutcomes
}

// Get fetches the record identified by rec from the host. rec must carry an ID
// (obtained from a prior Create or Find). Pass WithPortals, WithPortalLimit,
// WithPortalOffset, and WithResponseLayout to shape the returned data, or
// WithScript and friends to run scripts with the request; script outcomes are
// returned in the GetResponse.
func (c *Client) Get(ctx context.Context, rec Record, opts ...GetOption) (GetResponse, error) {
	if rec.id == "" {
		return GetResponse{}, errors.New("filemaker: record has no ID; create or find it first")
	}
	return c.GetByID(ctx, rec.layout, rec.id, opts...)
}

// GetByID fetches a single record addressed by layout and id. See Get for the
// option documentation. For a record returned by a prior operation, prefer Get —
// it sources the layout and ID from the record itself.
func (c *Client) GetByID(ctx context.Context, layout, id string, opts ...GetOption) (GetResponse, error) {
	switch {
	case layout == "":
		return GetResponse{}, errors.New("filemaker: no layout specified")
	case id == "":
		return GetResponse{}, errors.New("filemaker: no record id specified")
	}

	cfg, err := resolveGetConfig(opts)
	if err != nil {
		return GetResponse{}, err
	}

	u := c.recordURL(layout, id)
	if q := cfg.getQueryParams(); len(q) > 0 {
		u += "?" + q.Encode()
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodGet, u, nil, &rb); err != nil {
		return GetResponse{}, err
	}

	if len(rb.Response.Data) == 0 {
		return GetResponse{}, errors.New("filemaker: get returned no record data")
	}
	w := rb.Response.Data[0]
	record := Record{
		id:         w.ID,
		modID:      w.ModID,
		layout:     layout,
		fieldData:  w.FieldData,
		portalData: w.PortalData,
		loc:        c.location,
	}
	return GetResponse{Record: record, Scripts: rb.scriptOutcomes()}, nil
}

// getQueryParams encodes the read-shaping options as URL query parameters for
// the GET single-record request. Portal names are JSON-encoded (the wire format
// the host expects for that query key); per-portal paging uses
// _offset.<name>/_limit.<name> (with a leading underscore, unlike the Find body
// keys which omit it).
func (c recordConfig) getQueryParams() url.Values {
	v := url.Values{}
	if len(c.portals) > 0 {
		b, _ := json.Marshal(c.portals)
		v.Set("portal", string(b))
	}
	for name, pr := range c.portalRanges {
		if pr.offset > 0 {
			v.Set("_offset."+name, strconv.Itoa(pr.offset))
		}
		if pr.limit > 0 {
			v.Set("_limit."+name, strconv.Itoa(pr.limit))
		}
	}
	if c.responseLayout != "" {
		v.Set("layout.response", c.responseLayout)
	}
	for _, kv := range c.scriptParams() {
		v.Set(kv[0], kv[1])
	}
	return v
}
