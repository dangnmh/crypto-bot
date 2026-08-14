package toobit

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	infraws "crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"

	"crypto-bot/pkg/xjson"
)

// WsAdapter implements ws.ExchangeAdapter for Toobit.
type WsAdapter struct {
	pool         *pkgws.Pool
	client       *Client
	privateURL   string
	apiKey       string
	apiSecret    string
	cancelKeep   context.CancelFunc
	cancelKeepMu sync.Mutex
	clock        exchange.Clock
	priceCache   *infraws.PriceCache
}

// NewWsAdapter creates a new WsAdapter.
func NewWsAdapter(privateURL string) *WsAdapter {
	return &WsAdapter{
		privateURL: privateURL,
		priceCache: infraws.NewPriceCache(),
		clock:      exchange.RealClock{},
	}
}

// SetPool injects the websocket pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

func (a *WsAdapter) SubscribePublic(ctx context.Context, topic string, msg any) error {
	if a.pool == nil {
		return nil
	}
	return a.pool.SubscribePublic(ctx, topic, msg)
}

func (a *WsAdapter) UnsubscribePublic(ctx context.Context, topic string, msg any) error {
	if a.pool == nil {
		return nil
	}
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SetClient injects the REST client reference.
func (a *WsAdapter) SetClient(client *Client) {
	a.client = client
}

// SetClock configures a custom clock for testing.
func (a *WsAdapter) SetClock(clk exchange.Clock) {
	if clk != nil {
		a.clock = clk
	}
}

// SubscribeTicker streams bookTicker and realtimes.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	// 1. bookTicker
	bookMsg := map[string]any{
		eventKey:  eventSub,
		topicKey:  topicBookTicker,
		symbolKey: symbol,
	}
	if err := a.pool.SubscribePublic(ctx, symbol+":"+topicBookTicker, bookMsg); err != nil {
		return err
	}

	// 2. realtimes
	realtimesMsg := map[string]any{
		eventKey:  eventSub,
		topicKey:  topicRealtimes,
		symbolKey: symbol,
		paramsKey: map[string]any{
			binaryKey: false,
		},
	}
	if err := a.pool.SubscribePublic(ctx, symbol+":"+topicRealtimes, realtimesMsg); err != nil {
		return err
	}

	return nil
}

// UnsubscribeTicker stops streaming ticker updates.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	// 1. bookTicker
	bookMsg := map[string]any{
		eventKey:  cancelKey,
		topicKey:  topicBookTicker,
		symbolKey: symbol,
	}
	err1 := a.pool.UnsubscribePublic(ctx, symbol+":"+topicBookTicker, bookMsg)

	// 2. realtimes
	realtimesMsg := map[string]any{
		eventKey:  cancelKey,
		topicKey:  topicRealtimes,
		symbolKey: symbol,
		paramsKey: map[string]any{
			binaryKey: false,
		},
	}
	err2 := a.pool.UnsubscribePublic(ctx, symbol+":"+topicRealtimes, realtimesMsg)

	if err1 != nil {
		return err1
	}
	return err2
}

// SubscribePersonal is a no-op since connecting to user stream with listenKey automatically subscribes.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	return nil
}

func (a *WsAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
}

// GetPingConfig returns application level ping parameters.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	// Return a ping payload with a fixed timestamp for simplicity.
	return map[string]any{pingKey: 1700000000000}, 30 * time.Second
}

// GetAuthHook returns nil as authentication is done via listenKey URL path.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	a.apiKey = apiKey
	a.apiSecret = apiSecret
	return nil
}

// GetChannelExtractor maps WebSocket event keys to handler channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return extractChannel
}

func extractChannel(data []byte) string {
	if bytes.Contains(data, []byte(`"pong"`)) {
		return channelPong
	}
	if bytes.Contains(data, []byte(`"ping"`)) {
		return channelPing
	}

	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return ""
	}

	if data[0] == '[' {
		return extractArrayChannel(data)
	}

	return extractObjectChannel(data)
}

func extractArrayChannel(data []byte) string {
	var list []struct {
		Event     string       `json:"e"`
		EventTime xjson.Number `json:"E"`
	}
	if err := xjson.Unmarshal(data, &list); err != nil {
		slog.Error("unmarshal extractArrayChannel error", slog.String("exchange", "toobit"), slog.Any("error", err), slog.String("data", string(data)))
		return ""
	}
	if len(list) == 0 {
		return ""
	}
	if list[0].Event == outboundContractPositionInfo {
		return channelPersonalPosition
	}
	return ""
}

func extractObjectChannel(data []byte) string {
	var msg struct {
		Topic string `json:"topic"`
		Event string `json:"event"`
	}
	if err := xjson.Unmarshal(data, &msg); err == nil {
		if msg.Topic == topicBookTicker || msg.Topic == topicRealtimes {
			return channelTicker
		}
		if msg.Event == outboundContractPositionInfo || msg.Topic == outboundContractPositionInfo {
			return channelPersonalPosition
		}
	}
	return ""
}

type wsBookTickerData struct {
	Symbol string `json:"s"`
	Bid    string `json:"b"`
	Ask    string `json:"a"`
}

type wsRealtimesData struct {
	Symbol    string `json:"s"`
	LastPrice string `json:"c"`
	Volume    string `json:"v"`
}

// handleBookTicker parses and merges bookTicker data.
func (a *WsAdapter) handleBookTicker(sym string, rawData json.RawMessage) (string, *store.PriceData, error) {
	if len(rawData) == 0 || bytes.Equal(bytes.TrimSpace(rawData), []byte("[]")) {
		return "", nil, nil
	}

	var bt wsBookTickerData
	if err := xjson.Unmarshal(rawData, &bt); err != nil {
		return "", nil, fmt.Errorf("unmarshal bookTicker data: %w", err)
	}
	if sym == "" {
		sym = bt.Symbol
	}
	if sym == "" {
		return "", nil, fmt.Errorf("no symbol found in bookTicker payload")
	}

	pd := a.priceCache.UpdateDepthAndMidPrice(sym, decmath.ParseFloat(bt.Bid), decmath.ParseFloat(bt.Ask))
	return sym, pd, nil
}

// handleRealtimes parses and merges realtimes data.
func (a *WsAdapter) handleRealtimes(sym string, rawData json.RawMessage) (string, *store.PriceData, error) {
	var list []wsRealtimesData
	if err := xjson.Unmarshal(rawData, &list); err != nil {
		return "", nil, fmt.Errorf("unmarshal realtimes data: %w", err)
	}
	if len(list) == 0 {
		return "", nil, nil
	}
	rt := list[0]
	if sym == "" {
		sym = rt.Symbol
	}
	if sym == "" {
		return "", nil, fmt.Errorf("no symbol found in realtimes payload")
	}

	pd := a.priceCache.UpdateTicker(sym, decmath.ParseFloat(rt.LastPrice), 0, decmath.ParseFloat(rt.Volume))
	return sym, pd, nil
}

// ParseTicker unmarshals and merges bookTicker and realtimes streams.
func (a *WsAdapter) ParseTicker(data []byte) (string, *store.PriceData, error) {
	var basic struct {
		Topic  string          `json:"topic"`
		Symbol string          `json:"symbol"`
		Data   json.RawMessage `json:"data"`
	}
	if err := xjson.Unmarshal(data, &basic); err != nil {
		return "", nil, fmt.Errorf("unmarshal ticker basic: %w", err)
	}

	switch basic.Topic {
	case "bookTicker":
		return a.handleBookTicker(basic.Symbol, basic.Data)
	case "realtimes":
		return a.handleRealtimes(basic.Symbol, basic.Data)
	default:
		return "", nil, fmt.Errorf("unknown ticker topic: %s", basic.Topic)
	}
}

type wsPositionData struct {
	Event     string       `json:"e"`
	Symbol    string       `json:"s"`
	Side      string       `json:"S"`
	AvgPrice  string       `json:"p"`
	Position  string       `json:"P"`
	Pnl       string       `json:"up"`
	Leverage  string       `json:"v"`
	EventTime xjson.Number `json:"E"`
}

// ParsePosition parses position update pushes.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	if a.client != nil {
		a.client.logger.Debug("raw pos data", slog.String("exchange", "toobit"), slog.String("data", string(data)))
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, fmt.Errorf("empty position update payload")
	}

	var list []wsPositionData
	if err := xjson.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("unmarshal position: %w", err)
	}
	if len(list) == 0 {
		return nil, fmt.Errorf("empty position list")
	}
	raw := list[len(list)-1]

	if raw.Symbol == "" {
		return nil, fmt.Errorf("no symbol found in position update")
	}

	vol := decmath.ParseFloat(raw.Position)
	pType := exchange.PositionTypeLong
	if raw.Side == posSideShort {
		pType = exchange.PositionTypeShort
	}

	avgPrice := decmath.ParseFloat(raw.AvgPrice)
	pnl := decmath.ParseFloat(raw.Pnl)

	var lev int
	if raw.Leverage != "" {
		if val, err := strconv.ParseFloat(raw.Leverage, 64); err == nil {
			lev = int(val)
		}
	}

	var eventTime int64
	if tVal, err := raw.EventTime.Int64(); err == nil {
		eventTime = tVal
	}

	return &exchange.PersonalPositionUpdate{
		Symbol:          raw.Symbol,
		HoldVolContract: vol,
		PositionType:    pType,
		HoldAvgPrice:    avgPrice,
		OpenAvgPrice:    avgPrice,
		CloseProfitLoss: pnl,
		Leverage:        lev,
		UpdateTime:      eventTime,
	}, nil
}

// GetPrivateURLFunc implements PrivateURLProvider.
func (a *WsAdapter) GetPrivateURLFunc(ctx context.Context) func() (string, error) {
	return func() (string, error) {
		if a.client == nil {
			return "", fmt.Errorf("toobit client not injected in WsAdapter")
		}

		// 1. Fetch listenKey
		listenKey, err := a.client.CreateListenKey(ctx)
		if err != nil {
			return "", fmt.Errorf("create toobit listen key failed: %w", err)
		}

		// 2. Prevent goroutine leaks
		a.cancelKeepMu.Lock()
		if a.cancelKeep != nil {
			a.cancelKeep()
		}
		keepCtx, cancel := context.WithCancel(ctx)
		a.cancelKeep = cancel
		a.cancelKeepMu.Unlock()

		// 3. Start keepalive loop
		go a.keepAliveLoop(keepCtx, listenKey)

		// 4. Construct URL
		base := a.privateURL
		if base == "" {
			base = "wss://stream.toobit.com"
		}
		base = strings.TrimSuffix(base, "/")
		if !strings.Contains(base, "/api/v1/ws") {
			base += "/api/v1/ws"
		}

		return base + "/" + listenKey, nil
	}
}

func (a *WsAdapter) keepAliveLoop(ctx context.Context, listenKey string) {
	ticker := time.NewTicker(20 * time.Minute)
	defer ticker.Stop()

	a.client.logger.DebugContext(ctx, "⏳ Started keepalive loop for Toobit user stream")

	for {
		select {
		case <-ctx.Done():
			a.client.logger.DebugContext(context.WithoutCancel(ctx), "⏳ Stopped keepalive loop for Toobit user stream")
			return
		case <-ticker.C:
			if err := a.client.KeepAliveListenKey(ctx, listenKey); err != nil {
				a.client.logger.ErrorContext(ctx, "🔴 Failed to keepalive Toobit user stream", slog.Any("error", err))
			} else {
				a.client.logger.DebugContext(ctx, "🟢 Successfully kept alive Toobit user stream")
			}
		}
	}
}
