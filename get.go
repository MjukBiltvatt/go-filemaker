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

	p, err := resolveOptions(opts, GetOption.applyGet)
	if err != nil {
		return GetResponse{}, err
	}

	u := c.recordURL(layout, id)
	if q := p.getQuery(); len(q) > 0 {
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

// getQuery encodes the read-shaping options as URL query parameters for the GET
// single-record request. Portal names are JSON-encoded (the wire format the host
// expects for that query key); per-portal paging uses _offset.<name>/_limit.<name>
// (with a leading underscore, unlike the Find body keys which omit it).
func (p params) getQuery() url.Values {
	v := url.Values{}
	if len(p.portals) > 0 {
		b, _ := json.Marshal(p.portals)
		v.Set("portal", string(b))
	}
	for name, pr := range p.portalRanges {
		if pr.offset > 0 {
			v.Set("_offset."+name, strconv.Itoa(pr.offset))
		}
		if pr.limit > 0 {
			v.Set("_limit."+name, strconv.Itoa(pr.limit))
		}
	}
	if p.responseLayout != "" {
		v.Set("layout.response", p.responseLayout)
	}
	for _, kv := range p.scripts() {
		v.Set(kv[0], kv[1])
	}
	return v
}

// GetRangeResponse is the result of a GetRange. Records is empty (non-nil) when
// the layout contains no records. DataInfo carries the host's record counts.
// Scripts holds the outcomes of any scripts run with the request.
type GetRangeResponse struct {
	Records  []Record
	DataInfo DataInfo
	Scripts  ScriptOutcomes
}

// GetRange returns records from the layout in insertion order, shaped by the
// options. Without WithLimit the host returns at most 100 records; pass
// WithLimit (and WithOffset to page) to retrieve more. Pass WithSort to order
// the result, WithPortals and friends to control related records, or WithScript
// and friends to run scripts with the request; script outcomes are returned in
// the GetRangeResponse.
//
// Unlike Find, GetRange does not filter: it always returns the full record set
// (subject to limit and offset), making it the right tool for reading all
// records from a small layout or paginating without a find expression.
func (c *Client) GetRange(ctx context.Context, layout string, opts ...GetRangeOption) (GetRangeResponse, error) {
	if layout == "" {
		return GetRangeResponse{}, errors.New("filemaker: no layout specified")
	}

	p, err := resolveOptions(opts, GetRangeOption.applyGetRange)
	if err != nil {
		return GetRangeResponse{}, err
	}

	u := c.recordsURL(layout)
	if q := p.getRangeQuery(); len(q) > 0 {
		u += "?" + q.Encode()
	}

	var rb responseBody
	// ErrNoRecords means the layout has no records; treat it as an empty result
	// (non-nil slice) rather than surfacing the error, mirroring Find's behavior.
	if err := c.do(ctx, http.MethodGet, u, nil, &rb); err != nil && !errors.Is(err, ErrNoRecords) {
		return GetRangeResponse{}, err
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
	return GetRangeResponse{Records: records, DataInfo: rb.Response.DataInfo, Scripts: rb.scriptOutcomes()}, nil
}

// getRangeQuery encodes the options as URL query parameters for the GET records
// endpoint. Top-level pagination and sort use underscore-prefixed keys
// (_offset, _limit, _sort); portal filtering and paging share the same keys as
// the get-single endpoint (portal, _offset.<name>, _limit.<name>). Sort is
// JSON-encoded as an array, matching the wire format the host expects.
func (p params) getRangeQuery() url.Values {
	v := url.Values{}
	if p.offset > 0 {
		v.Set("_offset", strconv.Itoa(p.offset))
	}
	if p.limit > 0 {
		v.Set("_limit", strconv.Itoa(p.limit))
	}
	if len(p.sort) > 0 {
		b, _ := json.Marshal(p.sort)
		v.Set("_sort", string(b))
	}
	if len(p.portals) > 0 {
		b, _ := json.Marshal(p.portals)
		v.Set("portal", string(b))
	}
	for name, pr := range p.portalRanges {
		if pr.offset > 0 {
			v.Set("_offset."+name, strconv.Itoa(pr.offset))
		}
		if pr.limit > 0 {
			v.Set("_limit."+name, strconv.Itoa(pr.limit))
		}
	}
	if p.responseLayout != "" {
		v.Set("layout.response", p.responseLayout)
	}
	for _, kv := range p.scripts() {
		v.Set(kv[0], kv[1])
	}
	return v
}
