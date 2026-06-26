package filemaker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
)

// FindResponse is the result of a Find. Records is empty (non-nil) when no
// records match. DataInfo carries the host's record counts.
type FindResponse struct {
	Records  []Record
	DataInfo DataInfo
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

// CreateResponse is the host's acknowledgement of a Create. The client does not
// fetch the new record's field data; issue a Find for that.
type CreateResponse struct {
	RecordID string
	ModID    string
}

// UpdateResponse is the host's acknowledgement of an Update.
type UpdateResponse struct {
	ModID string
}

// Find runs the query against the layout. A query that matches no records
// returns a FindResponse with an empty Records slice and a nil error.
func (c *Client) Find(ctx context.Context, layout string, query Query) (FindResponse, error) {
	if layout == "" {
		return FindResponse{}, errors.New("filemaker: no layout specified")
	}

	body, err := json.Marshal(query)
	if err != nil {
		return FindResponse{}, fmt.Errorf("filemaker: failed to marshal query: %w", err)
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodPost, c.findURL(layout), body, &rb); err != nil {
		if errors.Is(err, ErrNoRecords) {
			return FindResponse{Records: []Record{}}, nil
		}
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
	return FindResponse{Records: records, DataInfo: rb.Response.DataInfo}, nil
}

// Create inserts a new record with the given field data and returns the host's
// acknowledgement (record ID and mod ID).
func (c *Client) Create(ctx context.Context, layout string, fields FieldData) (CreateResponse, error) {
	if layout == "" {
		return CreateResponse{}, errors.New("filemaker: no layout specified")
	}

	body, err := marshalRecordBody(fields, nil, "")
	if err != nil {
		return CreateResponse{}, err
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodPost, c.recordsURL(layout), body, &rb); err != nil {
		return CreateResponse{}, err
	}
	return CreateResponse{RecordID: rb.Response.RecordID, ModID: rb.Response.ModID}, nil
}

// UpdateOption configures an Update or UpdateByID.
type UpdateOption func(*updateConfig)

// updateConfig holds the optional parameters applied by UpdateOption values.
// Optimistic concurrency is one flag plus a version: conditional means a mod-ID
// check is wanted, and modID is the version to check against (empty means
// "source it from the record"). Both WithModID and IfUnchanged set conditional,
// so the options are order-independent. err carries deferred option validation
// (an UpdateOption cannot return an error directly), surfaced when resolved.
type updateConfig struct {
	conditional bool
	modID       string
	portalData  PortalData
	err         error
}

// WithModID makes the update conditional (optimistic concurrency) against a
// specific mod ID: the host rejects it with ErrRecordModified if the record's
// current mod ID differs — i.e. it changed since modID was read. modID must be
// non-empty; an empty one is reported as an error from Update/UpdateByID. To
// lock against the record you are updating, prefer IfUnchanged.
func WithModID(modID string) UpdateOption {
	return func(c *updateConfig) {
		if modID == "" {
			c.err = errors.New("filemaker: WithModID requires a non-empty mod ID")
			return
		}
		c.conditional = true
		c.modID = modID
	}
}

// IfUnchanged makes the update conditional on the record not having changed
// since it was read: it locks against the record's own ModID, so the host
// rejects the write with ErrRecordModified if another writer modified the record
// in the meantime. It is the ergonomic form of WithModID(rec.ModID()).
//
// Only the record-based Update can honor it (UpdateByID has no record to read a
// ModID from, and reports an error); a record without a ModID is likewise an
// error rather than a silent unconditional write. Combining it with WithModID is
// redundant — the explicit version from WithModID is used, regardless of order.
func IfUnchanged() UpdateOption {
	return func(c *updateConfig) {
		c.conditional = true
	}
}

// WithPortalData attaches related-record edits to an update, applied by the host
// alongside the field-data patch. Pass the rows in the same shape Record.Portals
// returns: a portal name mapped to its rows, each row's field values keyed by
// fully qualified name ("TableOccurrence::FieldName"). A row carrying a record ID
// (a plain "recordId" key) edits that existing related record — add a plain
// "modId" for optimistic locking — and a row without one is added as a new
// related record. Note the record ID is not table-occurrence qualified like the
// field values are: the host reads "TableOccurrence::recordId" as a field and
// rejects the edit with code 102 ("Field is missing").
//
// Only the named portal rows are touched; rows you omit are left unchanged. To
// remove related records, set "deleteRelated" in the FieldData patch (e.g.
// "Orders.3", or a slice for several): it is a field-data directive, not a portal
// edit. To edit only portals and leave the record's own fields untouched, pass a
// nil or empty FieldData. See the Claris Data API guide's "Edit record" page.
func WithPortalData(portals PortalData) UpdateOption {
	return func(c *updateConfig) {
		c.portalData = portals
	}
}

// resolveUpdateConfig applies the options and resolves the mod ID. A conditional
// update with no explicit version sources it from rec (nil for the id-addressed
// path, which cannot honor IfUnchanged). Deferred option errors surface here.
func resolveUpdateConfig(opts []UpdateOption, rec *Record) (updateConfig, error) {
	var cfg updateConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.err != nil {
		return cfg, cfg.err
	}
	if cfg.conditional && cfg.modID == "" {
		switch {
		case rec == nil:
			return cfg, errors.New("filemaker: IfUnchanged requires a record; use WithModID with UpdateByID")
		case rec.modID == "":
			return cfg, errors.New("filemaker: IfUnchanged requires a record with a ModID")
		}
		cfg.modID = rec.modID
	}
	return cfg, nil
}

// Update writes the given field data to the record, identified by rec, and
// returns the new mod ID. fields is a patch: only the named fields are written,
// and the rest of the record is left unchanged on the host. rec is used solely
// to address the record (its Layout and ID); its own field values are not sent.
// Writes are unconditional by default; pass IfUnchanged for optimistic
// concurrency against the record's ModID, or WithPortalData to edit related
// records in the same request.
func (c *Client) Update(ctx context.Context, rec Record, fields FieldData, opts ...UpdateOption) (UpdateResponse, error) {
	if rec.id == "" {
		return UpdateResponse{}, errors.New("filemaker: record has no ID; create or find it first")
	}
	// IfUnchanged is record-relative, so resolve it here (UpdateByID has no record
	// to read a ModID from) and append the resolved lock as an explicit WithModID.
	// Every other option passes through untouched, so new UpdateOptions need no
	// change here; delegating also keeps layout/id validation and the write in one
	// place, like Delete and UploadToContainer.
	cfg, err := resolveUpdateConfig(opts, &rec)
	if err != nil {
		return UpdateResponse{}, err
	}
	if cfg.conditional {
		// Full-slice expression so the append never mutates the caller's array.
		opts = append(opts[:len(opts):len(opts)], WithModID(cfg.modID))
	}
	return c.UpdateByID(ctx, rec.layout, rec.id, fields, opts...)
}

// UpdateByID writes the given field data to an existing record addressed by
// layout and id, and returns the new mod ID. See Update for the patch semantics.
// For optimistic concurrency pass WithModID (IfUnchanged needs a record); pass
// WithPortalData to edit related records in the same request.
func (c *Client) UpdateByID(ctx context.Context, layout, id string, fields FieldData, opts ...UpdateOption) (UpdateResponse, error) {
	switch {
	case layout == "":
		return UpdateResponse{}, errors.New("filemaker: no layout specified")
	case id == "":
		return UpdateResponse{}, errors.New("filemaker: no record id specified")
	}

	cfg, err := resolveUpdateConfig(opts, nil)
	if err != nil {
		return UpdateResponse{}, err
	}

	body, err := marshalRecordBody(fields, cfg.portalData, cfg.modID)
	if err != nil {
		return UpdateResponse{}, err
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodPatch, c.recordURL(layout, id), body, &rb); err != nil {
		if errors.Is(err, ErrRecordModified) {
			return UpdateResponse{}, fmt.Errorf("filemaker: record %q in layout %q: %w", id, layout, ErrRecordModified)
		}
		return UpdateResponse{}, err
	}
	return UpdateResponse{ModID: rb.Response.ModID}, nil
}

// Delete removes the record identified by rec.
func (c *Client) Delete(ctx context.Context, rec Record) error {
	if rec.id == "" {
		return errors.New("filemaker: record has no ID; create or find it first")
	}
	return c.DeleteByID(ctx, rec.layout, rec.id)
}

// DeleteByID removes a record addressed by layout and id.
func (c *Client) DeleteByID(ctx context.Context, layout, id string) error {
	switch {
	case layout == "":
		return errors.New("filemaker: no layout specified")
	case id == "":
		return errors.New("filemaker: no record id specified")
	}

	var rb responseBody
	return c.do(ctx, http.MethodDelete, c.recordURL(layout, id), nil, &rb)
}

// UploadToContainer uploads data to a container field of the record identified
// by rec. The record must already exist (created or returned by a find).
func (c *Client) UploadToContainer(ctx context.Context, rec Record, field, filename string, data io.Reader) error {
	if rec.id == "" {
		return errors.New("filemaker: record has no ID; create or find it first")
	}
	return c.UploadToContainerByID(ctx, rec.layout, rec.id, field, filename, data)
}

// UploadToContainerByID uploads data to a container field of an existing record
// addressed by layout and id.
func (c *Client) UploadToContainerByID(ctx context.Context, layout, id, field, filename string, data io.Reader) error {
	switch {
	case layout == "":
		return errors.New("filemaker: no layout specified")
	case id == "":
		return errors.New("filemaker: no record id specified")
	case field == "":
		return errors.New("filemaker: no container field specified")
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("upload", filename)
	if err != nil {
		return fmt.Errorf("filemaker: failed to build upload: %w", err)
	}
	if _, err := io.Copy(part, data); err != nil {
		return fmt.Errorf("filemaker: failed to read upload data: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("filemaker: failed to finalize upload: %w", err)
	}

	contentType := w.FormDataContentType()
	body := buf.Bytes()
	var rb responseBody
	return c.withAuth(ctx, func() (string, error) {
		return c.attempt(ctx, http.MethodPost, c.containerURL(layout, id, field), contentType, body, &rb)
	})
}

// DownloadFromContainer downloads the binary contents of a container field of
// the record identified by rec. The field must hold a container streaming URL
// (the value FileMaker returns for a container field); an empty or non-string
// field is reported as an error.
func (c *Client) DownloadFromContainer(ctx context.Context, rec Record, field string) ([]byte, error) {
	u, err := rec.StringE(field)
	if err != nil || u == "" {
		return nil, fmt.Errorf("filemaker: field %q is not a container URL", field)
	}
	return c.DownloadFromContainerByURL(ctx, u)
}

// DownloadFromContainerByURL downloads the binary contents of a container from
// its streaming URL. The URL must be on the session host (typically obtained
// from a record via record.String(field)); the bearer token is never sent to a
// foreign host.
func (c *Client) DownloadFromContainerByURL(ctx context.Context, containerURL string) ([]byte, error) {
	if containerURL == "" {
		return nil, errors.New("filemaker: empty container url")
	}
	if !strings.HasPrefix(containerURL, c.host) {
		return nil, fmt.Errorf("filemaker: refusing to fetch container from foreign host: %s", containerURL)
	}

	if err := c.ensureAuthenticated(ctx); err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, containerURL, nil)
	if err != nil {
		return nil, fmt.Errorf("filemaker: failed to build request: %w", err)
	}
	c.mu.RLock()
	token := c.token
	c.mu.RUnlock()
	req.Header.Set("Authorization", "Bearer "+token)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("filemaker: request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("filemaker: failed to fetch container data: %s", res.Status)
	}

	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("filemaker: failed to read container data: %w", err)
	}

	c.mu.Lock()
	c.lastActivity = time.Now()
	c.mu.Unlock()
	return data, nil
}

// marshalRecordBody wraps fields in the {"fieldData": ...} envelope the host
// expects, optionally including portal edits and a modId for optimistic locking
// (each omitted when empty). A nil fields map becomes an empty object so the host
// applies defaults on create and leaves the record's own fields untouched on a
// portal-only edit.
func marshalRecordBody(fields FieldData, portals PortalData, modID string) ([]byte, error) {
	if fields == nil {
		fields = FieldData{}
	}
	body, err := json.Marshal(struct {
		FieldData  FieldData  `json:"fieldData"`
		PortalData PortalData `json:"portalData,omitempty"`
		ModID      string     `json:"modId,omitempty"`
	}{fields, portals, modID})
	if err != nil {
		return nil, fmt.Errorf("filemaker: failed to marshal field data: %w", err)
	}
	return body, nil
}

// recordsURL is the collection endpoint for a layout (used to create records).
func (c *Client) recordsURL(layout string) string {
	return fmt.Sprintf("%s/layouts/%s/records", c.baseURL(), layout)
}

// recordURL is the endpoint for a single record by ID.
func (c *Client) recordURL(layout, id string) string {
	return fmt.Sprintf("%s/layouts/%s/records/%s", c.baseURL(), layout, id)
}

// findURL is the find endpoint for a layout.
func (c *Client) findURL(layout string) string {
	return fmt.Sprintf("%s/layouts/%s/_find", c.baseURL(), layout)
}

// containerURL is the upload endpoint for a record's container field.
func (c *Client) containerURL(layout, id, field string) string {
	return fmt.Sprintf("%s/layouts/%s/records/%s/containers/%s", c.baseURL(), layout, id, field)
}
