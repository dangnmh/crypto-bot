package futures

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"
)

var (
	_ exchange.WSTradeExecutor = (*TradeWSClient)(nil)

	// ErrWSTradeNotReady is returned when attempting to trade over an unauthenticated or disconnected WebSocket.
	ErrWSTradeNotReady = errors.New("bybit ws trade is not ready (mode=ws)")
)

// bybitWSHeader represents request headers for Bybit WebSocket trade.
type bybitWSHeader struct {
	Timestamp  string `json:"X-BAPI-TIMESTAMP"`
	RecvWindow string `json:"X-BAPI-RECV-WINDOW,omitempty"`
	Referer    string `json:"Referer,omitempty"`
}

// bybitWSTradeRequest represents an outbound WebSocket trade command.
type bybitWSTradeRequest struct {
	ReqID  string        `json:"reqId"`
	Header bybitWSHeader `json:"header"`
	Op     string        `json:"op"`
	Args   []any         `json:"args"`
}

// bybitWSHeaderResp represents response headers returned in Bybit WebSocket trade messages.
type bybitWSHeaderResp struct {
	TraceID   string `json:"Traceid"`
	Timenow   string `json:"Timenow"`
	Timestamp string `json:"X-BAPI-TIMESTAMP,omitempty"`
}

// bybitWSTradeResponse represents an inbound response from Bybit WebSocket trade.
type bybitWSTradeResponse struct {
	ReqID   string            `json:"reqId"`
	RetCode int               `json:"retCode"`
	RetMsg  string            `json:"retMsg"`
	Op      string            `json:"op"`
	Data    json.RawMessage   `json:"data"`
	Header  bybitWSHeaderResp `json:"header"`
	ConnID  string            `json:"connId"`
}

// TradeWSClient manages a persistent, authenticated WebSocket connection for Bybit V5 Trade.
// It wraps *pkgws.Client to reuse connection management, ping/pong heartbeats, and auto-reconnection,
// matching the pattern used in WsAdapter.
type TradeWSClient struct {
	client        *pkgws.Client
	apiKey        string
	apiSecret     string
	clock         exchange.Clock
	logger        *slog.Logger
	authenticated atomic.Bool
	reqCounter    atomic.Uint64
	closeOnce     sync.Once
}

// NewTradeWSClient creates a new Bybit TradeWSClient.
func NewTradeWSClient(wsURL, apiKey, apiSecret string, clock exchange.Clock, logger *slog.Logger) *TradeWSClient {
	if logger == nil {
		logger = slog.Default().With("component", "bybit_trade_ws")
	}
	if clock == nil {
		clock = exchange.RealClock{}
	}

	c := &TradeWSClient{
		apiKey:    apiKey,
		apiSecret: apiSecret,
		clock:     clock,
		logger:    logger,
	}

	pingPayload, pingInterval := GetBybitPingConfig()
	opts := []pkgws.ClientOption{
		pkgws.WithPing(pingPayload, pingInterval),
		pkgws.WithPongDetector(IsBybitPong),
		pkgws.WithRequestIDExtractor(ExtractBybitReqID),
		pkgws.WithOnConnected(c.onConnected),
		pkgws.WithOnDisconnected(c.onDisconnected),
	}

	c.client = pkgws.NewClient(wsURL, logger, opts...)
	c.client.SetGlobalHandler(c.handleMessage)
	return c
}

// onConnected is fired immediately after the underlying WebSocket connection is established.
// It generates the HMAC SHA-256 signature and sends the authentication payload.
func (c *TradeWSClient) onConnected(client *pkgws.Client) {
	c.authenticated.Store(false)
	if c.apiKey == "" || c.apiSecret == "" {
		// Empty credentials (e.g. test dummy)
		c.authenticated.Store(true)
		return
	}

	authMsg := BuildBybitAuthMessage(c.apiKey, c.apiSecret, c.clock.Now().UnixMilli())
	if err := client.SendJSON(authMsg); err != nil {
		c.logger.Error("Bybit Trade WS auth send failed", slog.Any("error", err))
	}
}

// onDisconnected is fired when the underlying WebSocket connection is severed.
// It invalidates the auth flag; pending in-flight requests are automatically aborted by pkg/ws.Client.
func (c *TradeWSClient) onDisconnected(_ *pkgws.Client) {
	c.authenticated.Store(false)
	c.logger.Warn("🟡 Bybit Trade WS connection lost")
}

// handleMessage processes inbound frames from the global handler (e.g. auth acknowledgements).
func (c *TradeWSClient) handleMessage(data []byte) {
	if isAuth, success, retCode, retMsg, connID := ParseBybitAuthResponse(data); isAuth {
		if success {
			c.authenticated.Store(true)
			c.logger.Info("🟢 Bybit Trade WS authenticated successfully", slog.String("connId", connID))
		} else {
			c.authenticated.Store(false)
			c.logger.Error("🔴 Bybit Trade WS authentication failed",
				slog.Int("retCode", retCode),
				slog.String("retMsg", retMsg),
			)
		}
	}
}

// IsReady returns true if the WebSocket connection is established and authenticated.
func (c *TradeWSClient) IsReady() bool {
	return c.client.IsConnected() && c.authenticated.Load()
}

// Start begins the connection and auto-reconnection loop in the background via pkg/ws.Client.
func (c *TradeWSClient) Start(ctx context.Context) {
	c.client.Connect(ctx)
}

// dispatch sends a WebSocket trade operation and awaits its correlated response via pkg/ws.Client.RoundTrip.
func (c *TradeWSClient) dispatch(ctx context.Context, op string, args []any) (*bybitWSTradeResponse, error) {
	if !c.IsReady() {
		return nil, ErrWSTradeNotReady
	}

	reqID := fmt.Sprintf("t-%d-%d", c.clock.Now().UnixMilli(), c.reqCounter.Add(1))
	ts := strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
	wsReq := bybitWSTradeRequest{
		ReqID: reqID,
		Header: bybitWSHeader{
			Timestamp:  ts,
			RecvWindow: "5000",
		},
		Op:   op,
		Args: args,
	}

	rawResp, err := c.client.RoundTrip(ctx, reqID, wsReq)
	if err != nil {
		if errors.Is(err, pkgws.ErrNotConnected) {
			return nil, ErrWSTradeNotReady
		}
		return nil, err
	}

	var resp bybitWSTradeResponse
	if err := xjson.Unmarshal(rawResp, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal ws trade resp: %w", err)
	}

	return &resp, nil
}

// CreateOrder executes order creation via WebSocket.
func (c *TradeWSClient) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	rawReq := buildBybitCreateOrderRequest(req)
	resp, err := c.dispatch(ctx, "order.create", []any{rawReq})
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	if resp.RetCode != 0 {
		return exchange.CreateOrderResult{}, fmt.Errorf("bybit ws create order error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}

	var res bybitCreateOrderResult
	if err := xjson.Unmarshal(resp.Data, &res); err != nil {
		return exchange.CreateOrderResult{}, fmt.Errorf("bybit ws parse create order result: %w", err)
	}

	var exchTime time.Time
	tsStr := resp.Header.Timenow
	if tsStr == "" {
		tsStr = resp.Header.Timestamp
	}
	if tsStr != "" {
		if ms, parseErr := strconv.ParseInt(tsStr, 10, 64); parseErr == nil {
			exchTime = time.UnixMilli(ms)
		}
	}

	return exchange.CreateOrderResult{
		OrderID:       res.OrderID,
		Time:          exchTime,
		TPSLSubmitted: false,
	}, nil
}

// CancelOrder cancels an order via WebSocket.
func (c *TradeWSClient) CancelOrder(ctx context.Context, symbol, orderID string) error {
	rawReq := bybitCancelOrderRequest{
		Category: categoryLinear,
		Symbol:   symbol,
		OrderID:  orderID,
	}

	resp, err := c.dispatch(ctx, "order.cancel", []any{rawReq})
	if err != nil {
		return err
	}

	if resp.RetCode != 0 {
		if resp.RetCode == 110001 || strings.Contains(strings.ToLower(resp.RetMsg), "already cancelled") || strings.Contains(strings.ToLower(resp.RetMsg), "filled") {
			return nil
		}
		return fmt.Errorf("bybit ws cancel order error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}

	return nil
}

// PrepareOrder pre-builds an order for low-latency dispatch over WebSocket.
func (c *TradeWSClient) PrepareOrder(ctx context.Context, req exchange.SubmitOrderRequest) (func(context.Context) (exchange.CreateOrderResult, error), error) {
	if !c.IsReady() {
		return nil, ErrWSTradeNotReady
	}

	return func(execCtx context.Context) (exchange.CreateOrderResult, error) {
		return c.CreateOrder(execCtx, req)
	}, nil
}

// LatencyMs returns the rolling median or last measured WebSocket round-trip latency in milliseconds.
func (c *TradeWSClient) LatencyMs() int64 {
	if c == nil || c.client == nil {
		return -1
	}
	return c.client.LatencyMs()
}

// MinLatencyMs returns the rolling minimum WebSocket round-trip latency in milliseconds.
func (c *TradeWSClient) MinLatencyMs() int64 {
	if c == nil || c.client == nil {
		return -1
	}
	return c.client.MinLatencyMs()
}

// MedianLatencyMs returns the rolling median WebSocket round-trip latency in milliseconds.
func (c *TradeWSClient) MedianLatencyMs() int64 {
	if c == nil || c.client == nil {
		return -1
	}
	return c.client.MedianLatencyMs()
}

// LastLatencyMs returns the raw single-ping round-trip latency of the most recent WebSocket pong.
func (c *TradeWSClient) LastLatencyMs() int64 {
	if c == nil || c.client == nil {
		return -1
	}
	return c.client.LastLatencyMs()
}

// Ping sends an immediate WebSocket ping to measure network latency.
func (c *TradeWSClient) Ping(ctx context.Context) error {
	if c == nil || c.client == nil {
		return ErrWSTradeNotReady
	}
	return c.client.Ping(ctx)
}

// Close closes the WebSocket connection and aborts pending dispatchers.
func (c *TradeWSClient) Close() {
	c.closeOnce.Do(func() {
		c.client.Close()
		c.authenticated.Store(false)
	})
}
