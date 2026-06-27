package filemaker

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// ProductInfo describes the FileMaker Data API engine running on the host, as
// reported by the productInfo metadata endpoint: its name and the patterns the
// host uses for date, time, and timestamp values. Every field is a string and
// is populated by the endpoint.
type ProductInfo struct {
	Name            string `json:"name"`
	DateFormat      string `json:"dateFormat"`
	TimeFormat      string `json:"timeFormat"`
	TimeStampFormat string `json:"timeStampFormat"`
}

// Database is a single FileMaker database hosted on the server and enabled for
// Data API access, as listed by Databases. Only its name is reported.
type Database struct {
	Name string `json:"name"`
}

// Script is an entry in a database's script catalog as listed by Scripts:
// either a runnable script or a script folder. When IsFolder is false the entry
// is a script and FolderScriptNames is empty; when IsFolder is true the entry is
// a folder and FolderScriptNames holds its contents, which may themselves be
// folders, nesting arbitrarily deep.
type Script struct {
	Name              string   `json:"name"`
	IsFolder          bool     `json:"isFolder"`
	FolderScriptNames []Script `json:"folderScriptNames"`
}

// Layout is an entry in a database's layout catalog as listed by Layouts:
// either a layout or a layout folder. When IsFolder is false the entry is a
// layout and FolderLayoutNames is empty; when IsFolder is true the entry is a
// folder and FolderLayoutNames holds its contents, which may themselves be
// folders, nesting arbitrarily deep.
type Layout struct {
	Name              string   `json:"name"`
	IsFolder          bool     `json:"isFolder"`
	FolderLayoutNames []Layout `json:"folderLayoutNames"`
}

// FieldMetadata describes a single field as it appears on a layout, reported by
// LayoutMetadata both for the layout's own fields and, within PortalMetadata,
// for the fields of each related portal. The booleans read out the field's
// definition (Global, AutoEnter) and validation (NotEmpty, Numeric,
// FourDigitYear). ValueList is empty when no value list is attached and
// MaxCharacters is 0 when no character limit is set; the repetition fields are
// 1-based (a non-repeating field reports MaxRepeat, RepetitionStart, and
// RepetitionEnd all as 1).
type FieldMetadata struct {
	Name            string `json:"name"`
	Type            string `json:"type"`        // Storage class, e.g. "normal", "calculation", "summary"
	DisplayType     string `json:"displayType"` // Control style, e.g. "editText", "popupList", "checkBox"
	Result          string `json:"result"`      // Data type, e.g. "text", "number", "date", "time", "timeStamp", "container"
	ValueList       string `json:"valueList"`   // Attached value list name, empty when none
	Global          bool   `json:"global"`
	AutoEnter       bool   `json:"autoEnter"`
	FourDigitYear   bool   `json:"fourDigitYear"`
	MaxRepeat       int    `json:"maxRepeat"`
	MaxCharacters   int    `json:"maxCharacters"`
	NotEmpty        bool   `json:"notEmpty"`
	Numeric         bool   `json:"numeric"`
	TimeOfDay       bool   `json:"timeOfDay"`
	RepetitionStart int    `json:"repetitionStart"`
	RepetitionEnd   int    `json:"repetitionEnd"`
}

// ValueListItem is a single entry of a ValueList. Value is the value stored in
// the field; DisplayValue is what the user sees. The two differ when the value
// list shows values from a second field; otherwise they are equal.
type ValueListItem struct {
	Value        string `json:"value"`
	DisplayValue string `json:"displayValue"`
}

// ValueList is a value list available on a layout, with its evaluated entries.
// Custom value lists are always populated; value lists based on a field's values
// are dynamic and only evaluated when a record is supplied (see
// WithValueListRecordID) — without one their Values come back empty.
type ValueList struct {
	Name   string          `json:"name"`
	Type   string          `json:"type"` // E.g. "customList" or "byField"
	Values []ValueListItem `json:"values"`
}

// LayoutMetadata is the full metadata for a single layout, as returned by the
// Client's LayoutMetadata method: the fields placed on the layout, the fields of
// each related portal keyed by the portal's table-occurrence name, and the value
// lists available on the layout.
type LayoutMetadata struct {
	FieldMetadata  []FieldMetadata            `json:"fieldMetaData"`
	PortalMetadata map[string][]FieldMetadata `json:"portalMetaData"`
	ValueLists     []ValueList                `json:"valueLists"`
}

// Databases lists the databases hosted on the server that are enabled for
// FileMaker Data API access.
//
// Like ProductInfo, this is a host-level metadata call: it is not scoped to the
// client's database, establishes no session, and does not update LastActivity.
//
// Authentication is conditional on the host's "Filter Databases in Client
// Applications" setting. When enabled, the host requires HTTP Basic credentials
// and returns only the databases the account may access; when disabled, no
// credentials are needed and every hosted database is listed. The endpoint uses
// Basic auth with the raw account credentials, not a session token, so Databases
// sends the client's username/password whenever a username is set — covering the
// enabled case — while a client built without credentials can still call it
// against a host that has filtering disabled.
func (c *Client) Databases(ctx context.Context) ([]Database, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL()+"/databases", nil)
	if err != nil {
		return nil, fmt.Errorf("filemaker: failed to build request: %w", err)
	}
	// Basic auth (not the session bearer token) per the endpoint's spec; sent
	// only when credentials exist, so a credential-free client still works
	// against a host with database filtering disabled.
	if c.username != "" {
		req.Header.Set("Authorization", "Basic "+basicAuth(c.username, c.password))
	}

	var rb responseBody
	if err := c.send(req, &rb); err != nil {
		return nil, err
	}
	return rb.Response.Databases, nil
}

// ProductInfo fetches metadata about the FileMaker Data API engine on the host:
// its name and the date/time formats it uses.
//
// The endpoint is not scoped to a database and requires no authentication, so
// ProductInfo neither establishes nor touches a session — it makes a single
// unauthenticated request and does not update LastActivity. This makes it a
// cheap connectivity check that can run before any login.
func (c *Client) ProductInfo(ctx context.Context) (ProductInfo, error) {
	// The endpoint takes no request body and no headers (the host documents its
	// HTTP header as "None"), so neither Content-Type nor Authorization is set.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.apiURL()+"/productInfo", nil)
	if err != nil {
		return ProductInfo{}, fmt.Errorf("filemaker: failed to build request: %w", err)
	}

	var rb responseBody
	if err := c.send(req, &rb); err != nil {
		return ProductInfo{}, err
	}
	return rb.Response.ProductInfo, nil
}

// Scripts lists the scripts defined in the client's database, as reported by the
// scripts metadata endpoint. The catalog preserves the database's script-folder
// hierarchy: a folder entry (IsFolder true) carries its contents in
// FolderScriptNames, nested arbitrarily deep, while a leaf entry is a runnable
// script.
//
// Unlike the host-level ProductInfo and Databases metadata calls, this endpoint
// is scoped to the client's database and authenticated with the session bearer
// token, so it establishes a session on first use (and triggers reauth like any
// other database operation) and counts as session activity, updating
// LastActivity.
func (c *Client) Scripts(ctx context.Context) ([]Script, error) {
	var rb responseBody
	if err := c.do(ctx, http.MethodGet, c.baseURL()+"/scripts", nil, &rb); err != nil {
		return nil, err
	}
	return rb.Response.Scripts, nil
}

// Layouts lists the layouts defined in the client's database, as reported by the
// layouts metadata endpoint. The catalog preserves the database's layout-folder
// hierarchy: a folder entry (IsFolder true) carries its contents in
// FolderLayoutNames, nested arbitrarily deep, while a leaf entry is a layout.
//
// Like Scripts, and unlike the host-level ProductInfo and Databases metadata
// calls, this endpoint is scoped to the client's database and authenticated with
// the session bearer token, so it establishes a session on first use (and
// triggers reauth like any other database operation) and counts as session
// activity, updating LastActivity.
func (c *Client) Layouts(ctx context.Context) ([]Layout, error) {
	var rb responseBody
	if err := c.do(ctx, http.MethodGet, c.baseURL()+"/layouts", nil, &rb); err != nil {
		return nil, err
	}
	return rb.Response.Layouts, nil
}

// LayoutMetadataOption configures a LayoutMetadata call.
type LayoutMetadataOption func(*layoutMetadataConfig)

// layoutMetadataConfig holds the optional parameters applied by
// LayoutMetadataOption values.
type layoutMetadataConfig struct {
	recordID string
}

// WithValueListRecordID supplies a record ID against which the host evaluates
// dynamic (field-based) value lists, sending it as the endpoint's recordId query
// parameter. Without it those value lists return no entries; custom value lists
// are unaffected either way. The ID names a record in the layout's own table.
func WithValueListRecordID(recordID string) LayoutMetadataOption {
	return func(c *layoutMetadataConfig) {
		c.recordID = recordID
	}
}

// LayoutMetadata fetches the metadata for a single layout: the fields on it, the
// fields of each related portal, and the value lists available on it.
//
// Pass WithValueListRecordID to have the host evaluate dynamic value lists
// against a specific record; omitted, those value lists come back empty (custom
// value lists are returned regardless).
//
// Like Scripts and Layouts, and unlike the host-level ProductInfo and Databases
// metadata calls, this endpoint is scoped to the client's database and
// authenticated with the session bearer token, so it establishes a session on
// first use (and triggers reauth like any other database operation) and counts
// as session activity, updating LastActivity.
func (c *Client) LayoutMetadata(ctx context.Context, layout string, opts ...LayoutMetadataOption) (LayoutMetadata, error) {
	var cfg layoutMetadataConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}

	// Escape the layout name into the path so names with spaces or other special
	// characters (e.g. "Package Management") address correctly.
	u := c.baseURL() + "/layouts/" + url.PathEscape(layout)
	if cfg.recordID != "" {
		u += "?" + url.Values{"recordId": {cfg.recordID}}.Encode()
	}

	var rb responseBody
	if err := c.do(ctx, http.MethodGet, u, nil, &rb); err != nil {
		return LayoutMetadata{}, err
	}
	return LayoutMetadata{
		FieldMetadata:  rb.Response.FieldMetaData,
		PortalMetadata: rb.Response.PortalMetaData,
		ValueLists:     rb.Response.ValueLists,
	}, nil
}
