package ws

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/ticker"

	"github.com/gorilla/websocket"
)

// Message represents a generic MEXC WebSocket message.
type Message struct {
	Channel string          `json:"channel"`
	Symbol  string          `json:"symbol,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Ts      int64           `json:"ts,omitempty"`
}

// ... struct definitions ...
// WsOrderDeal represents the parsed data from push.personal.order.
type WsOrderDeal struct {
	Symbol       string      `json:"symbol"`
	OrderID      interface{} `json:"orderId"`
	Price        float64     `json:"price"`
	Vol          float64     `json:"vol"`
	Side         int         `json:"side"`
	DealAvgPrice float64     `json:"dealAvgPrice"`
	DealVol      float64     `json:"dealVol"`
	State        int         `json:"state"` // 2: filled partly, 3: filled, 4: canceled
	ExternalOID  string      `json:"externalOid"`
	TakerFee     float64     `json:"takerFee"`
	MakerFee     float64     `json:"makerFee"`
	Profit       float64     `json:"profit"`
}

// GetOrderID returns the order ID as a string, handling both string and numeric JSON formats.
func (w *WsOrderDeal) GetOrderID() string {
	if s, ok := w.OrderID.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", w.OrderID)
}

// Handler is a callback function for processing WebSocket messages.
type Handler func(msg Message)

// Client manages the WebSocket connection to MEXC.
type Client struct {
	url            string
	apiKey         string
	apiSecret      string
	conn           *websocket.Conn
	mu             sync.Mutex
	handlers       map[string]Handler // channel -> external handler
	handlerMu      sync.RWMutex
	msgHandlers    map[string]func(Message) // channel -> internal handler
	orderCallbacks map[string]func(WsOrderDeal)
	orderCbMu      sync.RWMutex
	connected      bool
	store          *store.GlobalStore
	logCfg         config.LoggingConfig
	logger         *slog.Logger
	done           chan struct{}
	ready          chan struct{}
	readyOnce      sync.Once
}

// NewClient creates a new WebSocket client.
func NewClient(wsURL, apiKey, apiSecret string, store *store.GlobalStore, logCfg config.LoggingConfig) *Client {
	c := &Client{
		url:            wsURL,
		apiKey:         apiKey,
		apiSecret:      apiSecret,
		handlers:       make(map[string]Handler),
		orderCallbacks: make(map[string]func(WsOrderDeal)),
		store:          store,
		logCfg:         logCfg,
		logger:         slog.Default().With("component", "websocket"),
		done:           make(chan struct{}),
		ready:          make(chan struct{}),
	}
	c.initMsgHandlers()
	return c
}

// initMsgHandlers registers internal message handlers by channel name.
func (c *Client) initMsgHandlers() {
	c.msgHandlers = map[string]func(Message){
		"push.ticker":            c.handleTicker,
		"push.personal.order":    c.handlePushPersonalOrder,
		"push.personal.position": c.handlePosition,
		"push.kline":             c.handlePushKline,
		"push.depth.full":        c.handlePushDepth,
		"push.depth.step":        c.handlePushDepth,
	}
}

func (c *Client) handleTicker(msg Message) {
	if msg.Symbol == "" {
		return
	}
	if c.logCfg.Ticker {
		c.logger.Info("push.ticker", "symbol", msg.Symbol, "data", string(msg.Data))
	}
	c.store.UpdatePriceFromWsTicker(msg.Symbol, msg.Data)
}

func (c *Client) handlePosition(msg Message) {
	if c.logCfg.Position {
		c.logger.Info("Raw personal position payload", "data", string(msg.Data))
	}
}

// Connect establishes the WebSocket connection and starts the read loop.
// Reconnects automatically on disconnect.
func (c *Client) Connect(ctx context.Context) {
	c.logger.Debug("📡 Connecting WebSocket...", "url", c.url)

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("📡 WebSocket stopped")
			return
		default:
		}

		err := c.dial()
		if err != nil {
			c.logger.Error("🔴 WebSocket connection failed", "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		c.logger.Debug("🟢 WebSocket connected")

		if c.apiKey != "" && c.apiSecret != "" {
			if err := c.login(); err != nil {
				c.logger.Error("🔴 WebSocket login failed", "error", err)
			} else {
				c.logger.Info("🟢 WebSocket authenticated")
			}
		}

		c.markReady()

		// Start heartbeat and read loop
		go c.heartbeat(ctx)
		c.readLoop(ctx)

		// If we get here, connection was lost
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()

		c.logger.Warn("🟡 WebSocket disconnected, reconnecting in 2s...")
		time.Sleep(2 * time.Second)
	}
}

// dial establishes the WebSocket connection.
func (c *Client) dial() error {
	conn, _, err := websocket.DefaultDialer.Dial(c.url, nil)
	if err != nil {
		return err
	}

	c.mu.Lock()
	c.conn = conn
	c.connected = true
	c.mu.Unlock()

	return nil
}

// heartbeat sends ping messages to keep the connection alive.
func (c *Client) heartbeat(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-ctx.Done():
		case <-c.done:
			cancel()
		}
	}()

	ticker.Run(ctx, 15*time.Second, func() bool {
		c.mu.Lock()
		if c.conn != nil {
			err := c.conn.WriteJSON(map[string]string{"method": "ping"})
			if err != nil {
				c.logger.Warn("🟡 Heartbeat ping failed", "error", err)
			}
		}
		c.mu.Unlock()
		return true
	})
}

// readLoop reads messages from the WebSocket and dispatches them.
func (c *Client) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()

		if conn == nil {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			c.logger.Warn("🟡 WebSocket read error", "error", err)
			return
		}

		c.processMessage(data)
	}
}

// processMessage parses and dispatches a single WebSocket message.
func (c *Client) processMessage(data []byte) {
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return // skip malformed messages
	}

	if msg.Channel == "pong" {
		return
	}

	// Dispatch to internal handlers
	if handler, ok := c.msgHandlers[msg.Channel]; ok {
		handler(msg)
	}

	// Dispatch to external registered handlers
	c.handlerMu.RLock()
	if handler, ok := c.handlers[msg.Channel]; ok {
		handler(msg)
	}
	c.handlerMu.RUnlock()
}

func (c *Client) handlePushKline(msg Message) {
	if msg.Symbol == "" {
		return
	}
	var kData struct {
		A float64 `json:"a"`
		C float64 `json:"c"`
		H float64 `json:"h"`
		L float64 `json:"l"`
		O float64 `json:"o"`
		T int64   `json:"t"`
		V float64 `json:"v"`
	}
	if err := json.Unmarshal(msg.Data, &kData); err == nil {
		k := exchange.Kline{
			Timestamp: kData.T * 1000, // WS might push seconds. We standardize to ms.
			Open:      kData.O,
			Close:     kData.C,
			High:      kData.H,
			Low:       kData.L,
			Volume:    kData.V,
			Amount:    kData.A,
		}
		c.store.AddKline(msg.Symbol, k)
	}
}

func (c *Client) handlePushDepth(msg Message) {
	if msg.Symbol == "" {
		return
	}
	var depthData struct {
		Asks    [][]float64 `json:"asks"`
		Bids    [][]float64 `json:"bids"`
		Version int64       `json:"version"`
	}
	if err := json.Unmarshal(msg.Data, &depthData); err == nil {
		ob := &exchange.OrderBook{
			Symbol:  msg.Symbol,
			Version: depthData.Version,
			Asks:    make([]exchange.OrderBookEntry, 0, len(depthData.Asks)),
			Bids:    make([]exchange.OrderBookEntry, 0, len(depthData.Bids)),
		}
		for _, ask := range depthData.Asks {
			if len(ask) >= 2 {
				ob.Asks = append(ob.Asks, exchange.OrderBookEntry{Price: ask[0], Volume: ask[1]})
			}
		}
		for _, bid := range depthData.Bids {
			if len(bid) >= 2 {
				ob.Bids = append(ob.Bids, exchange.OrderBookEntry{Price: bid[0], Volume: bid[1]})
			}
		}
		c.store.UpdateDepth(msg.Symbol, ob)
	}
}

func (c *Client) handlePushPersonalOrder(msg Message) {
	if c.logCfg.Order {
		c.logger.Info("Raw personal order payload", "data", string(msg.Data))
	}
	var deal WsOrderDeal
	if err := json.Unmarshal(msg.Data, &deal); err == nil {
		c.handleOrderDeal(deal)
	}
}

// handleOrderDeal executes the registered callback for a specific order ID.
func (c *Client) handleOrderDeal(deal WsOrderDeal) {
	oid := deal.GetOrderID()
	c.orderCbMu.RLock()
	cb, ok := c.orderCallbacks[oid]
	c.orderCbMu.RUnlock()

	if ok {
		// Execute callback asynchronously to avoid blocking the WS read loop
		go cb(deal)
	}
}

// OnOrderUpdate registers a callback for a specific order ID.
// The callback will be removed automatically after the specified timeout.
func (c *Client) OnOrderUpdate(orderID string, timeout time.Duration, callback func(WsOrderDeal)) {
	c.orderCbMu.Lock()
	c.orderCallbacks[orderID] = callback
	c.orderCbMu.Unlock()

	// Auto-cleanup after timeout to prevent memory leak
	go func() {
		time.Sleep(timeout)
		c.orderCbMu.Lock()
		delete(c.orderCallbacks, orderID)
		c.orderCbMu.Unlock()
	}()
}

// RemoveOrderCallback manually removes a registered callback.
func (c *Client) RemoveOrderCallback(orderID string) {
	c.orderCbMu.Lock()
	delete(c.orderCallbacks, orderID)
	c.orderCbMu.Unlock()
}

// login authenticates the connection for private channels.
func (c *Client) login() error {
	reqTime := fmt.Sprintf("%d", time.Now().UnixMilli())
	message := c.apiKey + reqTime
	mac := hmac.New(sha256.New, []byte(c.apiSecret))
	mac.Write([]byte(message))
	signature := hex.EncodeToString(mac.Sum(nil))

	msg := map[string]interface{}{
		"method": "login",
		"param": map[string]interface{}{
			"apiKey":    c.apiKey,
			"reqTime":   reqTime,
			"signature": signature,
			"subscribe": false,
		},
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("connection is nil")
	}
	return c.conn.WriteJSON(msg)
}

// SubscribeOrderDeals starts receiving personal order events.
// Call this after login.
func (c *Client) SubscribeOrderDeals() error {
	msg := map[string]interface{}{
		"method": "personal.filter",
		"param": map[string]interface{}{
			"filters": []map[string]string{
				{"filter": "order"},
				{"filter": "position"},
			},
		},
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return fmt.Errorf("connection is nil")
	}
	c.logger.Debug("📡 Filtering WS to receive order push events")
	return c.conn.WriteJSON(msg)
}

// OnMessage registers a handler for a specific channel.
func (c *Client) OnMessage(channel string, handler Handler) {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	c.handlers[channel] = handler
}

// Subscribe sends a subscription request for a symbol+channel.
func (c *Client) Subscribe(symbol, channel string) error {
	param := map[string]string{
		"symbol": symbol,
	}
	if channel == "kline" {
		param["interval"] = "Min1"
	}

	msg := map[string]interface{}{
		"method": "sub." + channel,
		"param":  param,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	c.logger.Debug("📡 Subscribing", "symbol", symbol, "channel", channel)
	return c.conn.WriteJSON(msg)
}

// Unsubscribe sends an unsubscription request.
func (c *Client) Unsubscribe(symbol, channel string) error {
	msg := map[string]interface{}{
		"method": "unsub." + channel,
		"param": map[string]string{
			"symbol": symbol,
		},
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	return c.conn.WriteJSON(msg)
}

// SubscribeDepth subscribes to the depth channel for a symbol.
func (c *Client) SubscribeDepth(symbol string, step string) error {
	method := "sub.depth.full"
	param := map[string]interface{}{
		"symbol": symbol,
		"limit":  20,
	}

	if step != "" {
		method = "sub.depth.step"
		param["step"] = step
	}

	msg := map[string]interface{}{
		"method": method,
		"param":  param,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("connection is nil")
	}

	c.logger.Debug("📡 Subscribing", "symbol", symbol, "channel", "depth.full")
	return c.conn.WriteJSON(msg)
}

// UnsubscribeDepth unsubscribes from the depth channel.
func (c *Client) UnsubscribeDepth(symbol string, step string) error {
	method := "unsub.depth.full"
	param := map[string]interface{}{
		"symbol": symbol,
		"limit":  20,
	}

	if step != "" {
		method = "unsub.depth.step"
		param["step"] = step
	}

	msg := map[string]interface{}{
		"method": method,
		"param":  param,
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	return c.conn.WriteJSON(msg)
}

// SubscribeAll subscribes to ticker and kline for multiple symbols.
func (c *Client) SubscribeAll(symbols []string) {
	for _, sym := range symbols {
		if err := c.Subscribe(sym, "ticker"); err != nil {
			c.logger.Warn("🟡 Subscribe failed", "error", err, "symbol", sym, "channel", "ticker")
		}
		// Also subscribe to 1-minute klines for dynamic pricing
		if err := c.Subscribe(sym, "kline"); err != nil {
			c.logger.Warn("🟡 Subscribe failed", "error", err, "symbol", sym, "channel", "kline")
		}
	}
}

// UnsubscribeAll unsubscribes from all tracked symbols.
func (c *Client) UnsubscribeAll(symbols []string) {
	for _, sym := range symbols {
		_ = c.Unsubscribe(sym, "ticker")
	}
}

// IsConnected returns true if the WebSocket is currently connected.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Close closes the WebSocket connection.
func (c *Client) Close() {
	close(c.done)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
		c.connected = false
	}
}

// WaitReady blocks until the WebSocket connection is established and authenticated.
func (c *Client) WaitReady(ctx context.Context) {
	select {
	case <-c.ready:
	case <-ctx.Done():
	}
}

// markReady signals that the WebSocket is connected and ready for subscriptions.
func (c *Client) markReady() {
	c.readyOnce.Do(func() {
		close(c.ready)
		c.logger.Info("🟢 WS Client ready")
	})
}
