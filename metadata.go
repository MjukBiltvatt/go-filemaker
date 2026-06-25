package filemaker

import (
	"context"
	"fmt"
	"net/http"
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
