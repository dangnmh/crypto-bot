package toobit

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"crypto-bot/pkg/xjson"
)

type toobitListenKey struct {
	ListenKey string `json:"listenKey"`
}

type serverTimeResponse struct {
	ServerTime int64 `json:"serverTime"`
}

// Private raw methods.

func (c *Client) rawGetServerTime(ctx context.Context) (*serverTimeResponse, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v1/time", nil, false)
	if err != nil {
		return nil, err
	}
	var resp serverTimeResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal server time: %w", err)
	}
	return &resp, nil
}

func (c *Client) rawCreateListenKey(ctx context.Context) (*toobitListenKey, error) {
	body, err := c.request(ctx, http.MethodPost, "/api/v1/listenKey", nil, true)
	if err != nil {
		return nil, err
	}
	var res toobitListenKey
	if err := xjson.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("unmarshal listenKey: %w", err)
	}
	return &res, nil
}

func (c *Client) rawKeepAliveListenKey(ctx context.Context, listenKey string) error {
	params := map[string]string{
		"listenKey": listenKey,
	}
	_, err := c.request(ctx, http.MethodPut, "/api/v1/listenKey", params, true)
	return err
}

// Public mapper methods.

// SupportLeverageOnOrder returns false for Toobit.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

// WarmUp pings the server to warm up HTTP connections.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	_, _ = c.GetServerTime(ctx)
}

// GetServerTime returns the server millisecond timestamp.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	resp, err := c.rawGetServerTime(ctx)
	if err != nil {
		return 0, err
	}
	return resp.ServerTime, nil
}

// CreateListenKey creates a new user stream listen key.
func (c *Client) CreateListenKey(ctx context.Context) (string, error) {
	res, err := c.rawCreateListenKey(ctx)
	if err != nil {
		return "", err
	}
	return res.ListenKey, nil
}

// KeepAliveListenKey keeps the active user stream listen key alive.
func (c *Client) KeepAliveListenKey(ctx context.Context, listenKey string) error {
	return c.rawKeepAliveListenKey(ctx, listenKey)
}
