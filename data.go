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

// CreateResponse is the host's acknowledgement of a Create. The client does not
// fetch the new record's field data; issue a Find for that.
type CreateResponse struct {
	RecordID string
	ModID    string
	Scripts  ScriptOutcomes
}

// UpdateResponse is the host's acknowledgement of an Update.
type UpdateResponse struct {
	ModID   string
	Scripts ScriptOutcomes
}

// DeleteResponse is the host's acknowledgement of a Delete. The delete returns
// no record data; it carries the script outcomes when scripts were run.
type DeleteResponse struct {
	Scripts ScriptOutcomes
}

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

// Create inserts a new record with the given field data and returns the host's
// acknowledgement (record ID and mod ID). Pass WithPortalData to add related
// records, or WithScript and friends to run scripts, in the same request; script
// outcomes are returned in the CreateResponse.
func (c *Client) Create(ctx context.Context, layout string, fields FieldData, opts ...CreateOption) (CreateResponse, error) {
	if layout == "" {
		return CreateResponse{}, errors.New("filemaker: no layout specified")
	}

	cfg, err := resolveCreateConfig(opts)
	if err != nil {
		return CreateResponse{}, err
	}

	body, err := marshalRecordBody(fields, cfg, c.dateFormat)
	if err != nil {
		return CreateResponse{}, err
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodPost, c.recordsURL(layout), body, &rb); err != nil {
		return CreateResponse{}, err
	}
	return CreateResponse{RecordID: rb.Response.RecordID, ModID: rb.Response.ModID, Scripts: rb.scriptOutcomes()}, nil
}

// Update writes the given field data to the record, identified by rec, and
// returns the new mod ID. fields is a patch: only the named fields are written,
// and the rest of the record is left unchanged on the host. rec is used solely
// to address the record (its Layout and ID); its own field values are not sent.
// Writes are unconditional by default; pass IfUnchanged for optimistic
// concurrency against the record's ModID, WithPortalData to edit related records,
// or WithScript and friends to run scripts, in the same request; script outcomes
// are returned in the UpdateResponse.
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
// WithPortalData to edit related records, or WithScript and friends to run
// scripts, in the same request; script outcomes are returned in the
// UpdateResponse.
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

	body, err := marshalRecordBody(fields, cfg, c.dateFormat)
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
	return UpdateResponse{ModID: rb.Response.ModID, Scripts: rb.scriptOutcomes()}, nil
}

// Delete removes the record identified by rec. Pass WithScript and friends to
// run scripts with the request; their outcomes are returned in the
// DeleteResponse.
func (c *Client) Delete(ctx context.Context, rec Record, opts ...DeleteOption) (DeleteResponse, error) {
	if rec.id == "" {
		return DeleteResponse{}, errors.New("filemaker: record has no ID; create or find it first")
	}
	return c.DeleteByID(ctx, rec.layout, rec.id, opts...)
}

// DeleteByID removes a record addressed by layout and id. Pass WithScript and
// friends to run scripts with the request; unlike the other record writes the
// delete endpoint has no body, so those ride in the URL query string, and their
// outcomes are returned in the DeleteResponse.
func (c *Client) DeleteByID(ctx context.Context, layout, id string, opts ...DeleteOption) (DeleteResponse, error) {
	switch {
	case layout == "":
		return DeleteResponse{}, errors.New("filemaker: no layout specified")
	case id == "":
		return DeleteResponse{}, errors.New("filemaker: no record id specified")
	}

	cfg, err := resolveDeleteConfig(opts)
	if err != nil {
		return DeleteResponse{}, err
	}

	u := c.recordURL(layout, id)
	if q := cfg.queryParams(); len(q) > 0 {
		u += "?" + q.Encode()
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodDelete, u, nil, &rb); err != nil {
		return DeleteResponse{}, err
	}
	return DeleteResponse{Scripts: rb.scriptOutcomes()}, nil
}

// UploadToContainer uploads data to a container field of the record identified
// by rec. The record must already exist (created or returned by a find). Pass
// IfUnchanged (or WithModID) for optimistic concurrency against the record's
// current mod ID.
func (c *Client) UploadToContainer(ctx context.Context, rec Record, field, filename string, data io.Reader, opts ...UploadOption) error {
	if rec.id == "" {
		return errors.New("filemaker: record has no ID; create or find it first")
	}
	// IfUnchanged is record-relative, so resolve it here against rec and append
	// the resolved lock as an explicit WithModID, then delegate — mirroring
	// Update/UpdateByID.
	cfg, err := resolveUploadConfig(opts, &rec)
	if err != nil {
		return err
	}
	if cfg.conditional {
		// Full-slice expression so the append never mutates the caller's array.
		opts = append(opts[:len(opts):len(opts)], WithModID(cfg.modID))
	}
	return c.UploadToContainerByID(ctx, rec.layout, rec.id, field, filename, data, opts...)
}

// UploadToContainerByID uploads data to a container field of an existing record
// addressed by layout and id. For optimistic concurrency pass WithModID
// (IfUnchanged needs a record); the container endpoint has no JSON body, so the
// mod ID rides in the URL query string.
func (c *Client) UploadToContainerByID(ctx context.Context, layout, id, field, filename string, data io.Reader, opts ...UploadOption) error {
	switch {
	case layout == "":
		return errors.New("filemaker: no layout specified")
	case id == "":
		return errors.New("filemaker: no record id specified")
	case field == "":
		return errors.New("filemaker: no container field specified")
	}

	cfg, err := resolveUploadConfig(opts, nil)
	if err != nil {
		return err
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

	u := c.containerURL(layout, id, field)
	if q := cfg.queryParams(); len(q) > 0 {
		u += "?" + q.Encode()
	}

	contentType := w.FormDataContentType()
	body := buf.Bytes()
	var rb responseBody
	return c.withAuth(ctx, func() (string, error) {
		return c.attempt(ctx, http.MethodPost, u, contentType, body, &rb)
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

// SetGlobalFields sets the values of global fields in the database. Fields must
// use fully qualified names (Table::FieldName); values follow the same rules as
// regular field data (string for text/date/timestamp/time, float64 for number).
// Global fields persist for the duration of the session.
func (c *Client) SetGlobalFields(ctx context.Context, fields FieldData) error {
	if fields == nil {
		fields = FieldData{}
	}
	body, err := json.Marshal(map[string]any{"globalFields": fields})
	if err != nil {
		return fmt.Errorf("filemaker: failed to marshal global fields: %w", err)
	}
	var rb responseBody
	return c.do(ctx, http.MethodPatch, c.globalsURL(), body, &rb)
}

// marshalRecordBody wraps fields in the {"fieldData": ...} envelope the host
// expects, drawing the optional parameters (portal data, a modId for optimistic
// locking, and any script directives) from cfg and omitting each when unset. A
// nil fields map becomes an empty object so the host applies defaults on create
// and leaves the record's own fields untouched on a portal-only edit.
//
// When format is non-nil, Date/Timestamp wrapper values are rewritten to that
// format and a "dateformats" parameter is added so the host interprets the input
// accordingly; when nil, no parameter is sent — the host applies its default
// format (US) and the wrappers self-marshal in US to match (works on any server).
//
// The body is assembled as a map so the script directives (whose keys carry dots,
// e.g. "script.param") share one source of truth with the Delete query string:
// both read recordConfig.scriptParams.
func marshalRecordBody(fields FieldData, cfg recordConfig, format *DateFormat) ([]byte, error) {
	if fields == nil {
		fields = FieldData{}
	}
	portals := cfg.portalData

	var dateFormats *int
	if format != nil {
		fields = applyDateFormat(fields, *format)
		portals = applyDateFormatPortals(portals, *format)
		v := int(*format)
		dateFormats = &v
	}

	body := map[string]any{"fieldData": fields}
	if len(portals) > 0 {
		body["portalData"] = portals
	}
	if cfg.modID != "" {
		body["modId"] = cfg.modID
	}
	if opts := cfg.entryOptions(); len(opts) > 0 {
		body["options"] = opts
	}
	for _, kv := range cfg.scriptParams() {
		body[kv[0]] = kv[1]
	}
	if dateFormats != nil {
		body["dateformats"] = *dateFormats
	}

	out, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("filemaker: failed to marshal field data: %w", err)
	}
	return out, nil
}

// formatDateValue rewrites a Date/Timestamp wrapper to its string form in the
// given format (preserving the zero-time-clears-the-field convention). Any other
// value — including the Time/Duration wrappers, whose format does not depend on
// the date format — is returned unchanged to marshal itself.
func formatDateValue(v any, format DateFormat) any {
	switch val := v.(type) {
	case Date:
		t := time.Time(val)
		if t.IsZero() {
			return ""
		}
		return fmDate(t, format)
	case Timestamp:
		t := time.Time(val)
		if t.IsZero() {
			return ""
		}
		return fmTimestamp(t, format)
	default:
		return v
	}
}

// applyDateFormat returns a copy of fields with Date/Timestamp values rewritten
// in the given format. The caller's map is never mutated.
func applyDateFormat(fields FieldData, format DateFormat) FieldData {
	out := make(FieldData, len(fields))
	for k, v := range fields {
		out[k] = formatDateValue(v, format)
	}
	return out
}

// applyDateFormatPortals does the same as applyDateFormat but for portal rows,
// deep-copying so the caller's data is never mutated.
func applyDateFormatPortals(portals PortalData, format DateFormat) PortalData {
	if portals == nil {
		return nil
	}
	out := make(PortalData, len(portals))
	for name, rows := range portals {
		newRows := make([]map[string]any, len(rows))
		for i, row := range rows {
			nr := make(map[string]any, len(row))
			for k, v := range row {
				nr[k] = formatDateValue(v, format)
			}
			newRows[i] = nr
		}
		out[name] = newRows
	}
	return out
}
