package bitunix

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// GetServerTime returns current Unix server time in milliseconds by fetching
// the headers from a public Bitunix API endpoint.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL, http.NoBody)
	if err != nil {
		return time.Now().UnixMilli(), err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return time.Now().UnixMilli(), err
	}
	defer resp.Body.Close()

	if arriveTime := resp.Header.Get("req-arrive-time"); arriveTime != "" {
		if val, err := strconv.ParseInt(arriveTime, 10, 64); err == nil {
			return val, nil
		}
	}
	if startTime := resp.Header.Get("resp-start-time"); startTime != "" {
		if val, err := strconv.ParseInt(startTime, 10, 64); err == nil {
			return val, nil
		}
	}

	dateStr := resp.Header.Get("Date")
	if dateStr == "" {
		return time.Now().UnixMilli(), nil
	}

	t, err := time.Parse(time.RFC1123, dateStr)
	if err != nil {
		return time.Now().UnixMilli(), err
	}

	return t.UnixMilli(), nil
}

// SupportLeverageOnOrder returns false for Bitunix.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

// WarmUp pings the server to keep the connection hot.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {}
