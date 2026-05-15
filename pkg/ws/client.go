package ws

import (
	"context"
	"log/slog"
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
	onConnected      func(*Client) // Used for custom authentication logic immediately after dial
	onReady          func(*Client) // Called after each successful connection is ready
	pingPayload      interface{}   // Payload to send periodically. If nil, no ping is sent.
	pingPeriod       time.Duration
	channelExtractor func([]byte) string // Extracts routing key (channel/topic) from raw JSON
}

// ClientOption configures the generic WebSocket client.
type ClientOption func(*Client)

// WithChannelExtractor sets the function used to extract the routing channel from raw JSON payloads.
func WithChannelExtractor(extractor func([]byte) string) ClientOption {
	return func(c *Client) {
		c.channelExtractor = extractor
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
func WithPing(payload interface{}, period time.Duration) ClientOption {
	return func(c *Client) {
		c.pingPayload = payload
		c.pingPeriod = period
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
			if !waitContext(ctx, 2*time.Second) {
				return
			}
			continue
		}

		c.logger.Debug("🟢 WebSocket connected")

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

		c.logger.Warn("🟡 WebSocket disconnected, reconnecting in 2s...")
		if !waitContext(ctx, 2*time.Second) {
			return
		}
	}
}

// dial establishes the WebSocket connection.
func (c *Client) dial() error {
	conn, resp, err := websocket.DefaultDialer.Dial(c.url, nil)
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
			err := c.conn.WriteJSON(c.pingPayload)
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
func (c *Client) SendJSON(msg interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return nil
	}
	return c.conn.WriteJSON(msg)
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

func waitContext(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// WaitReady blocks until the WebSocket connection is established and ready.
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
