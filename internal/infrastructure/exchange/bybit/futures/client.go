package futures

import (
	"context"
	"fmt"
	"maps"
	"net/http"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/bybit"
	"crypto-bot/pkg/xjson"
)

var (
	_ exchange.Client                = (*Client)(nil)
	_ exchange.KlineProvider         = (*Client)(nil)
	_ exchange.TopGainerProvider     = (*Client)(nil)
	_ exchange.OrderExecutor         = (*Client)(nil)
	_ exchange.PreSignExecutor       = (*Client)(nil)
	_ exchange.ClosedPnLProvider     = (*Client)(nil)
	_ exchange.RawRequest            = (*Client)(nil)
	_ exchange.RawRequester          = (*Client)(nil)
	_ exchange.TradeModeConfigurable = (*Client)(nil)
	_ exchange.TPSLProvider          = (*Client)(nil)
)

// Client is the Bybit V5 Linear Perpetual Futures REST & WebSocket API client.
type Client struct {
	base      *bybit.BaseClient
	tradeMode exchange.TradeMode
	wsTrade   exchange.WSTradeExecutor
}

// NewClient creates a new Bybit Futures API client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret, accountType string, logCfg config.LoggingConfig) *Client {
	return &Client{
		base:      bybit.NewBaseClient(httpClient, baseURL, apiKey, apiSecret, accountType, logCfg),
		tradeMode: exchange.TradeModeHTTP,
	}
}

// SetTradeMode sets the trading execution transport mode (http or ws).
func (c *Client) SetTradeMode(mode exchange.TradeMode) {
	c.tradeMode = mode
}

// TradeMode returns the current trading execution transport mode.
func (c *Client) TradeMode() exchange.TradeMode {
	if c.tradeMode == "" {
		return exchange.TradeModeHTTP
	}
	return c.tradeMode
}

// SetWSTradeExecutor sets the WebSocket trade executor.
func (c *Client) SetWSTradeExecutor(executor exchange.WSTradeExecutor) {
	c.wsTrade = executor
}

// WSTradeExecutor returns the current WebSocket trade executor.
func (c *Client) WSTradeExecutor() exchange.WSTradeExecutor {
	return c.wsTrade
}

// Close closes any underlying active WebSocket trade connections.
func (c *Client) Close() {
	if c.wsTrade != nil {
		c.wsTrade.Close()
	}
}

// BaseClient returns the underlying shared Bybit BaseClient.
func (c *Client) BaseClient() *bybit.BaseClient {
	return c.base
}

// SetClock configures a custom clock implementation for testing.
func (c *Client) SetClock(clk exchange.Clock) {
	c.base.SetClock(clk)
}

// IsFutures returns true for Futures client.
func (c *Client) IsFutures() bool {
	return true
}

func (c *Client) PrepareRequest(ctx context.Context, method, path string, query map[string]string, body []byte) (func(context.Context) ([]byte, error), error) {
	return c.base.PrepareRequest(ctx, method, path, query, body)
}

func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	return c.base.RawRequest(ctx, method, path, query, body)
}

func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["category"] == "" {
		p["category"] = categoryLinear
	}
	return c.RawRequest(ctx, http.MethodGet, "/v5/market/tickers", p, nil)
}

func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["category"] == "" {
		p["category"] = categoryLinear
	}
	return c.RawRequest(ctx, http.MethodGet, "/v5/market/tickers", p, nil)
}

func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["category"] == "" {
		p["category"] = categoryLinear
	}
	return c.RawRequest(ctx, http.MethodGet, "/v5/position/list", p, nil)
}

func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["category"] == "" {
		p["category"] = categoryLinear
	}
	return c.RawRequest(ctx, http.MethodGet, "/v5/position/closed-pnl", p, nil)
}

func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["category"] == "" {
		p["category"] = categoryLinear
	}
	p["orderId"] = orderID
	return c.RawRequest(ctx, http.MethodGet, "/v5/order/realtime", p, nil)
}

func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["category"] == "" {
		p["category"] = categoryLinear
	}
	return c.RawRequest(ctx, http.MethodGet, "/v5/order/history", p, nil)
}

func (c *Client) GetOrderDealsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["category"] == "" {
		p["category"] = categoryLinear
	}
	return c.RawRequest(ctx, http.MethodGet, "/v5/execution/list", p, nil)
}

func (c *Client) GetClosedPnLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	if p["category"] == "" {
		p["category"] = categoryLinear
	}
	return c.RawRequest(ctx, http.MethodGet, "/v5/position/closed-pnl", p, nil)
}

func (c *Client) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	symbol := params["symbol"]
	orderID := params["order_id"]
	if orderID == "" {
		orderID = params["orderId"]
	}
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if orderID == "" {
		return nil, fmt.Errorf("order_id is required")
	}
	info, err := c.GetOrderPNL(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return xjson.Marshal(info)
}
