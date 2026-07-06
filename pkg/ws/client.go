package ws

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"crypto-bot/pkg/ticker"

	"github.com/gorilla/websocket"
)

// Handler is a callback function for processing raw WebSocket messages.
const msgPong = "pong"

type Handler func(data []byte)

// Client manages a generic WebSocket connection.
type Client struct {
	url           string
	conn          *websocket.Conn
	mu            sync.Mutex
	handlers      map[string]Handler // channel -> external handler
	handlerMu     sync.RWMutex
	globalHandler Handler // callback from Multiplexer/Pool
	connected     bool
	logger        *slog.Logger
	done          chan struct{}
	ready         chan struct{}
	readyOnce     sync.Once
	closeOnce     sync.Once

	// Hooks
	onConnected       func(*Client) // Used for custom authentication logic immediately after dial
	onReady           func(*Client) // Called after each successful connection is ready
	pingPayload       any           // Payload to send periodically. If nil, no ping is sent.
	pingPeriod        time.Duration
	channelExtractor  func([]byte) string // Extracts routing key (channel/topic) from raw JSON
	urlFunc           func() (string, error)
	preprocessor      func([]byte) ([]byte, error)
	headersFunc       func() (http.Header, error)
	customPingHandler func(*websocket.Conn, []byte) bool
}

// ClientOption configures the generic WebSocket client.
type ClientOption func(*Client)

// WithChannelExtractor sets the function used to extract the routing channel from raw JSON payloads.
func WithChannelExtractor(extractor func([]byte) string) ClientOption {
	return func(c *Client) {
		c.channelExtractor = extractor
	}
}

// WithCustomPingHandler sets a callback to handle custom server-initiated pings.
func WithCustomPingHandler(handler func(*websocket.Conn, []byte) bool) ClientOption {
	return func(c *Client) {
		c.customPingHandler = handler
	}
}

// WithOnConnected sets a callback fired immediately after the connection is established.
// Useful for sending authentication messages before marking the client as ready.
func WithOnConnected(hook func(*Client)) ClientOption {
	return func(c *Client) {
		c.onConnected = hook
	}
}

// WithOnReady sets a callback fired after each successful connection is ready.
func WithOnReady(hook func(*Client)) ClientOption {
	return func(c *Client) {
		c.onReady = hook
	}
}

// WithPing keeps the connection alive by periodically sending the specified payload.
func WithPing(payload any, period time.Duration) ClientOption {
	return func(c *Client) {
		c.pingPayload = payload
		c.pingPeriod = period
	}
}

// WithURLFunc sets a function used to dynamically generate the WebSocket URL before dial.
func WithURLFunc(urlFunc func() (string, error)) ClientOption {
	return func(c *Client) {
		c.urlFunc = urlFunc
	}
}

// WithPreprocessor sets a function to preprocess (e.g. decompress) raw messages after they are read.
func WithPreprocessor(preprocessor func([]byte) ([]byte, error)) ClientOption {
	return func(c *Client) {
		c.preprocessor = preprocessor
	}
}

// WithHeadersFunc sets a function to dynamically generate HTTP headers before dial.
func WithHeadersFunc(headersFunc func() (http.Header, error)) ClientOption {
	return func(c *Client) {
		c.headersFunc = headersFunc
	}
}

// NewClient creates a new generic WebSocket client.
func NewClient(wsURL string, logger *slog.Logger, opts ...ClientOption) *Client {
	if logger == nil {
		logger = slog.Default().With("component", "websocket")
	}

	c := &Client{
		url:      wsURL,
		handlers: make(map[string]Handler),
		logger:   logger,
		done:     make(chan struct{}),
		ready:    make(chan struct{}),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// SetGlobalHandler sets a fallback handler for all messages (used by Pool).
func (c *Client) SetGlobalHandler(handler Handler) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.globalHandler = handler
}

// URL returns the WebSocket endpoint URL.
func (c *Client) URL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.url
}

// Connect establishes the WebSocket connection and starts the read loop.
// Reconnects automatically on disconnect.
func (c *Client) Connect(ctx context.Context) {
	c.logger.DebugContext(ctx, "📡 Connecting WebSocket...", slog.String("url", c.url))

	for {
		select {
		case <-ctx.Done():
			c.logger.DebugContext(ctx, "📡 WebSocket stopped")
			return
		case <-c.done:
			c.logger.DebugContext(ctx, "📡 WebSocket closed")
			return
		default:
		}

		err := c.dial()
		if err != nil {
			c.logger.ErrorContext(ctx, "🔴 WebSocket connection failed", slog.Any("error", err))
			if !waitContextOrDone(ctx, c.done, 2*time.Second) {
				return
			}
			continue
		}

		c.logger.DebugContext(ctx, "🟢 WebSocket connected")

		if c.onConnected != nil {
			c.onConnected(c)
		}

		c.markReady()
		if c.onReady != nil {
			c.onReady(c)
		}

		connCtx, cancelConn := context.WithCancel(ctx)
		// Start heartbeat and read loop.
		if c.pingPayload != nil && c.pingPeriod > 0 {
			go c.heartbeat(connCtx)
		}
		c.readLoop(connCtx)
		cancelConn()

		// If we get here, connection was lost
		c.mu.Lock()
		c.connected = false
		c.mu.Unlock()

		c.logger.WarnContext(ctx, "🟡 WebSocket disconnected, reconnecting in 2s...")
		if !waitContextOrDone(ctx, c.done, 2*time.Second) {
			return
		}
	}
}

// dial establishes the WebSocket connection.
func (c *Client) dial() error {
	if c.urlFunc != nil {
		u, err := c.urlFunc()
		if err != nil {
			return fmt.Errorf("dynamic ws url gen: %w", err)
		}
		c.url = u
	}
	var headers http.Header
	if c.headersFunc != nil {
		h, err := c.headersFunc()
		if err != nil {
			return fmt.Errorf("dynamic ws headers gen: %w", err)
		}
		headers = h
	}
	conn, resp, err := websocket.DefaultDialer.Dial(c.url, headers)
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
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

	ticker.Run(ctx, c.pingPeriod, func() bool {
		c.mu.Lock()
		if c.conn != nil {
			var err error
			if strPayload, ok := c.pingPayload.(string); ok {
				err = c.conn.WriteMessage(websocket.TextMessage, []byte(strPayload))
			} else {
				var data []byte
				data, err = json.Marshal(c.pingPayload)
				if err == nil {
					err = c.conn.WriteMessage(websocket.TextMessage, data)
				}
			}
			if err != nil {
				c.logger.WarnContext(ctx, "🟡 Heartbeat ping failed", slog.Any("error", err))
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
			c.logger.WarnContext(ctx, "🟡 WebSocket read error", slog.Any("error", err))
			return
		}

		if c.preprocessor != nil {
			decompressed, err := c.preprocessor(data)
			if err != nil {
				c.logger.WarnContext(ctx, "Preprocess message failed", slog.Any("error", err))
				continue
			}
			data = decompressed
		}

		c.processMessage(data)
	}
}

// processMessage parses and dispatches a single WebSocket message.
func (c *Client) processMessage(data []byte) {
	if strings.ToLower(strings.TrimSpace(string(data))) == "pong" {
		return
	}
	if c.customPingHandler != nil {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn != nil && c.customPingHandler(conn, data) {
			return
		}
	}

	if strings.ToLower(strings.TrimSpace(string(data))) == "ping" {
		c.mu.Lock()
		if c.conn != nil {
			_ = c.conn.WriteMessage(websocket.TextMessage, []byte("Pong"))
		}
		c.mu.Unlock()
		return
	}

	c.logger.Debug("🚀 WebSocket message received", slog.String("data", string(data)))

	c.handleEventLog(data)
	if c.channelExtractor == nil {
		c.mu.Lock()
		gh := c.globalHandler
		c.mu.Unlock()
		if gh != nil {
			gh(data)
		}
		return
	}

	channel := c.channelExtractor(data)
	if channel == "" || channel == msgPong {
		return
	}

	// Dispatch to external registered handlers
	c.handlerMu.RLock()
	if handler, ok := c.handlers[channel]; ok {
		handler(data)
	}
	c.handlerMu.RUnlock()

	c.mu.Lock()
	gh := c.globalHandler
	c.mu.Unlock()
	if gh != nil {
		gh(data)
	}
}

// SendJSON sends a generic JSON payload.
func (c *Client) SendJSON(msg any) error {
	c.logger.Debug("SendJSON", slog.Any("msg", msg))
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.TextMessage, data)
}

// OnMessage registers a handler for a specific channel.
func (c *Client) OnMessage(channel string, handler Handler) {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	c.handlers[channel] = handler
}

// IsConnected returns true if the WebSocket is currently connected.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// Close closes the WebSocket connection.
func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.done)
	})
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
		c.connected = false
	}
}

func waitContextOrDone(ctx context.Context, done chan struct{}, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-done:
		return false
	case <-timer.C:
		return true
	}
}

// WaitReady blocks until the WebSocket connection is established and ready.
func (c *Client) WaitReady(ctx context.Context) error {
	select {
	case <-c.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// markReady signals that the WebSocket is connected and ready for subscriptions.
func (c *Client) markReady() {
	c.readyOnce.Do(func() {
		close(c.ready)
		c.logger.Info("🟢 WS Client ready")
	})
}

func (c *Client) handleEventLog(data []byte) {
	var eventHeader struct {
		Event string `json:"event"`
		Code  string `json:"code"`
		Msg   string `json:"msg"`
	}
	if err := json.Unmarshal(data, &eventHeader); err == nil && eventHeader.Event != "" {
		switch eventHeader.Event {
		case "error", "channel-conn-count-error":
			c.logger.Error("🔴 WebSocket event error received",
				slog.String("event", eventHeader.Event),
				slog.String("code", eventHeader.Code),
				slog.String("msg", eventHeader.Msg),
			)
		case "notice":
			c.logger.Warn("🟡 WebSocket notice received",
				slog.String("code", eventHeader.Code),
				slog.String("msg", eventHeader.Msg),
			)
		}
	}
}
