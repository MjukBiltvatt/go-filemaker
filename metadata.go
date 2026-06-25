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
