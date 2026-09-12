package binance

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

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"
)

var (
	_ exchange.WSTradeExecutor = (*TradeWSClient)(nil)

	// ErrWSTradeNotReady is returned when attempting to trade over an unauthenticated or disconnected WebSocket.
	ErrWSTradeNotReady = errors.New("binance ws trade is not ready (mode=ws)")
)

// binanceWSTradeRequest represents an outbound WebSocket trade command.
type binanceWSTradeRequest struct {
	ID     string         `json:"id"`
	Method string         `json:"method"`
	Params map[string]any `json:"params"`
}

// binanceWSError represents error information returned in Binance WebSocket trade messages.
type binanceWSError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

// binanceWSTradeResponse represents an inbound response from Binance WebSocket trade.
type binanceWSTradeResponse struct {
	ID     string          `json:"id"`
	Status int             `json:"status"`
	Result json.RawMessage `json:"result"`
	Error  *binanceWSError `json:"error,omitempty"`
}

// binanceWSOrderResult models the result returned from Binance order.place.
type binanceWSOrderResult struct {
	OrderID       int64  `json:"orderId"`
	Symbol        string `json:"symbol"`
	Status        string `json:"status"`
	ClientOrderID string `json:"clientOrderId"`
	UpdateTime    int64  `json:"updateTime"`
	Time          int64  `json:"time"`
}

// TradeWSClient manages a persistent, authenticated WebSocket connection for Binance USD(S)-M Futures Trade.
// It wraps *pkgws.Client to reuse connection management, ping/pong heartbeats, and auto-reconnection,
// matching the pattern used in Bybit TradeWSClient.
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

// NewTradeWSClient creates a new Binance TradeWSClient.
func NewTradeWSClient(wsURL, apiKey, apiSecret string, clock exchange.Clock, logger *slog.Logger) *TradeWSClient {
	if logger == nil {
		logger = slog.Default().With("component", "binance_trade_ws")
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

	pingPayload, pingInterval := GetBinancePingConfig()
	opts := []pkgws.ClientOption{
		pkgws.WithPing(pingPayload, pingInterval),
		pkgws.WithPongDetector(IsBinancePong),
		pkgws.WithRequestIDExtractor(ExtractBinanceReqID),
		pkgws.WithOnConnected(c.onConnected),
		pkgws.WithOnDisconnected(c.onDisconnected),
	}

	c.client = pkgws.NewClient(wsURL, logger, opts...)
	c.client.SetGlobalHandler(c.handleMessage)
	return c
}

// onConnected is fired immediately after the underlying WebSocket connection is established.
func (c *TradeWSClient) onConnected(client *pkgws.Client) {
	c.authenticated.Store(false)
	if c.apiKey == "" || c.apiSecret == "" {
		// Empty credentials (e.g. test dummy)
		c.authenticated.Store(true)
		return
	}

	reqID := fmt.Sprintf("auth-%d", c.clock.Now().UnixMilli())
	ts := c.clock.Now().UnixMilli()
	params := SignBinanceWSParams(nil, c.apiKey, c.apiSecret, ts)

	logonReq := binanceWSTradeRequest{
		ID:     reqID,
		Method: wsMethodSessionLogon,
		Params: params,
	}

	if err := client.SendJSON(logonReq); err != nil {
		c.logger.Error("Binance Trade WS logon send failed", slog.Any("error", err))
	}
}

// onDisconnected is fired when the underlying WebSocket connection is severed.
func (c *TradeWSClient) onDisconnected(_ *pkgws.Client) {
	c.authenticated.Store(false)
	c.logger.Warn("🟡 Binance Trade WS connection lost")
}

// handleMessage processes inbound frames from the global handler (e.g. session.logon acknowledgements).
func (c *TradeWSClient) handleMessage(data []byte) {
	var resp struct {
		ID     string          `json:"id"`
		Status int             `json:"status"`
		Error  *binanceWSError `json:"error,omitempty"`
	}
	if err := xjson.Unmarshal(data, &resp); err == nil && strings.HasPrefix(resp.ID, "auth-") {
		if resp.Status == 200 {
			c.authenticated.Store(true)
			c.logger.Info("🟢 Binance Trade WS authenticated successfully", slog.String("id", resp.ID))
		} else {
			c.authenticated.Store(false)
			var code int
			var msg string
			if resp.Error != nil {
				code = resp.Error.Code
				msg = resp.Error.Msg
			}
			c.logger.Error("🔴 Binance Trade WS authentication failed",
				slog.Int("status", resp.Status),
				slog.Int("code", code),
				slog.String("msg", msg),
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
func (c *TradeWSClient) dispatch(ctx context.Context, method string, rawParams map[string]any) (*binanceWSTradeResponse, error) {
	if !c.IsReady() {
		return nil, ErrWSTradeNotReady
	}

	reqID := fmt.Sprintf("t-%d-%d", c.clock.Now().UnixMilli(), c.reqCounter.Add(1))
	ts := c.clock.Now().UnixMilli()
	signedParams := SignBinanceWSParams(rawParams, c.apiKey, c.apiSecret, ts)

	wsReq := binanceWSTradeRequest{
		ID:     reqID,
		Method: method,
		Params: signedParams,
	}

	rawResp, err := c.client.RoundTrip(ctx, reqID, wsReq)
	if err != nil {
		if errors.Is(err, pkgws.ErrNotConnected) {
			return nil, ErrWSTradeNotReady
		}
		return nil, err
	}

	var resp binanceWSTradeResponse
	if err := xjson.Unmarshal(rawResp, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal ws trade resp: %w", err)
	}

	return &resp, nil
}

func buildBinanceCreateOrderWSParams(req exchange.SubmitOrderRequest) (map[string]any, error) {
	sdkSide, err := toBinanceSide(req.Side)
	if err != nil {
		return nil, err
	}
	sdkType, sdkTif := toBinanceOrderTypeAndTIF(req.Type)

	params := map[string]any{
		paramSymbol: req.Symbol,
		"side":      sdkSide,
		"type":      sdkType,
		"quantity":  req.Vol,
	}

	if sdkType != orderTypeMarket {
		params["price"] = req.Price
		params["timeInForce"] = sdkTif
	}

	if req.PositionMode == domain.PositionModeHedge {
		posSide := posSideLong
		if req.Side == exchange.SideOpenShort || req.Side == exchange.SideCloseShort {
			posSide = posSideShort
		}
		params["positionSide"] = posSide
	} else if req.ReduceOnly {
		params["reduceOnly"] = binanceTrueStr
	}

	if req.ExternalOID != "" {
		params["newClientOrderId"] = req.ExternalOID
	}

	return params, nil
}

// CreateOrder executes order creation via WebSocket.
func (c *TradeWSClient) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	rawParams, err := buildBinanceCreateOrderWSParams(req)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	resp, err := c.dispatch(ctx, wsMethodOrderPlace, rawParams)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	if resp.Status != 200 {
		var code int
		var msg string
		if resp.Error != nil {
			code = resp.Error.Code
			msg = resp.Error.Msg
		}
		return exchange.CreateOrderResult{}, fmt.Errorf("binance ws create order error (status=%d): code=%d, msg=%s", resp.Status, code, msg)
	}

	var res binanceWSOrderResult
	if err := xjson.Unmarshal(resp.Result, &res); err != nil {
		return exchange.CreateOrderResult{}, fmt.Errorf("binance ws parse create order result: %w", err)
	}

	orderID := strconv.FormatInt(res.OrderID, 10)
	var exchTime time.Time
	if res.UpdateTime > 0 {
		exchTime = time.UnixMilli(res.UpdateTime)
	} else if res.Time > 0 {
		exchTime = time.UnixMilli(res.Time)
	}

	return exchange.CreateOrderResult{
		OrderID:       orderID,
		Time:          exchTime,
		TPSLSubmitted: false,
	}, nil
}

// CancelOrder cancels an order via WebSocket.
func (c *TradeWSClient) CancelOrder(ctx context.Context, symbol, orderID string) error {
	rawParams := map[string]any{
		paramSymbol: symbol,
	}
	if id, err := strconv.ParseInt(orderID, 10, 64); err == nil {
		rawParams["orderId"] = id
	} else {
		rawParams["origClientOrderId"] = orderID
	}

	resp, err := c.dispatch(ctx, wsMethodOrderCancel, rawParams)
	if err != nil {
		return err
	}

	if resp.Status != 200 {
		if resp.Error != nil {
			msgLower := strings.ToLower(resp.Error.Msg)
			if resp.Error.Code == -2011 || strings.Contains(msgLower, "unknown order") || strings.Contains(msgLower, "filled") || strings.Contains(msgLower, "already") {
				return nil
			}
			return fmt.Errorf("binance ws cancel order error (status=%d): code=%d, msg=%s", resp.Status, resp.Error.Code, resp.Error.Msg)
		}
		return fmt.Errorf("binance ws cancel order error: status=%d", resp.Status)
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
