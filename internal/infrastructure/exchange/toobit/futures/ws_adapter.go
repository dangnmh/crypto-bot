package futures

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

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	infraws "crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"
)

var (
	_ exchange.DepthSubscriber = (*WsAdapter)(nil)
	_ exchange.DepthParser     = (*WsAdapter)(nil)
)

// WsAdapter implements ws.ExchangeAdapter for Toobit Futures.
type WsAdapter struct {
	pool         *pkgws.Pool
	client       *Client
	privateURL   string
	apiKey       string
	apiSecret    string
	clock        exchange.Clock
	priceCache   *infraws.PriceCache
	cancelKeep   context.CancelFunc
	cancelKeepMu sync.Mutex
}

// NewWsAdapter creates a new Toobit Futures WsAdapter.
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
	bookMsg := map[string]any{
		eventKey:  eventSub,
		topicKey:  topicBookTicker,
		symbolKey: symbol,
	}
	if err := a.SubscribePublic(ctx, symbol+":"+topicBookTicker, bookMsg); err != nil {
		return err
	}

	realtimesMsg := map[string]any{
		eventKey:  eventSub,
		topicKey:  topicRealtimes,
		symbolKey: symbol,
		paramsKey: map[string]any{
			binaryKey: false,
		},
	}
	return a.SubscribePublic(ctx, symbol+":"+topicRealtimes, realtimesMsg)
}

// UnsubscribeTicker stops streaming ticker updates.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	bookMsg := map[string]any{
		eventKey:  eventCancel,
		topicKey:  topicBookTicker,
		symbolKey: symbol,
	}
	err1 := a.UnsubscribePublic(ctx, symbol+":"+topicBookTicker, bookMsg)

	realtimesMsg := map[string]any{
		eventKey:  eventCancel,
		topicKey:  topicRealtimes,
		symbolKey: symbol,
		paramsKey: map[string]any{
			binaryKey: false,
		},
	}
	err2 := a.UnsubscribePublic(ctx, symbol+":"+topicRealtimes, realtimesMsg)

	if err1 != nil {
		return err1
	}
	return err2
}

// SubscribeDepth streams orderbook depth.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol string) error {
	msg := map[string]any{
		eventKey:  eventSub,
		topicKey:  topicDiffDepth,
		symbolKey: symbol,
		paramsKey: map[string]any{
			binaryKey: false,
		},
	}
	return a.SubscribePublic(ctx, symbol+":"+topicDiffDepth, msg)
}

// UnsubscribeDepth stops streaming orderbook depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol string) error {
	msg := map[string]any{
		eventKey:  eventCancel,
		topicKey:  topicDiffDepth,
		symbolKey: symbol,
		paramsKey: map[string]any{
			binaryKey: false,
		},
	}
	return a.UnsubscribePublic(ctx, symbol+":"+topicDiffDepth, msg)
}

// SubscribePersonal is a no-op for Toobit futures.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	return nil
}

func (a *WsAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
}

// GetPingConfig returns application level ping parameters.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return map[string]any{channelPing: 1700000000000}, 30 * time.Second
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
		return "pong"
	}
	if bytes.Contains(data, []byte(`"ping"`)) {
		return "ping"
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
		slog.Error("unmarshal extractArrayChannel error", slog.String("exchange", "toobit"), slog.Any("error", err))
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
		Code  any    `json:"code"`
		Desc  string `json:"desc"`
		Msg   string `json:"msg"`
	}
	if err := xjson.Unmarshal(data, &msg); err == nil {
		if msg.Topic == topicBookTicker || msg.Topic == topicRealtimes {
			return "ticker"
		}
		if msg.Topic == topicDepth || msg.Topic == topicDiffDepth || msg.Event == topicDepth {
			return topicDepth
		}
		if msg.Event == outboundContractPositionInfo || msg.Topic == outboundContractPositionInfo {
			return channelPersonalPosition
		}
		if msg.Code != nil {
			codeStr := fmt.Sprintf("%v", msg.Code)
			if codeStr != "" && codeStr != "0" && codeStr != "200" {
				desc := msg.Desc
				if desc == "" {
					desc = msg.Msg
				}
				slog.Warn("🟡 Toobit WS error received", slog.String("code", codeStr), slog.String("desc", desc))
			}
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

type wsDepthEntry struct {
	Bids [][]xjson.Number `json:"b"`
	Asks [][]xjson.Number `json:"a"`
	V    json.RawMessage  `json:"v"`
	T    int64            `json:"t"`
}

func parseToobitDepthVersion(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	s := strings.Trim(string(raw), "\"")
	if idx := strings.Index(s, "_"); idx != -1 {
		s = s[:idx]
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// ParseDepth parses depth messages into domain.OrderBook.
func (a *WsAdapter) ParseDepth(data []byte) (string, *domain.OrderBook, error) {
	var basic struct {
		Topic  string          `json:"topic"`
		Symbol string          `json:"symbol"`
		Data   json.RawMessage `json:"data"`
	}
	if err := xjson.Unmarshal(data, &basic); err != nil {
		return "", nil, fmt.Errorf("unmarshal depth basic: %w", err)
	}

	sym := basic.Symbol

	var entries []wsDepthEntry
	if err := xjson.Unmarshal(basic.Data, &entries); err != nil {
		var single wsDepthEntry
		if err2 := xjson.Unmarshal(basic.Data, &single); err2 != nil {
			return "", nil, fmt.Errorf("unmarshal depth data: %w", err)
		}
		entries = []wsDepthEntry{single}
	}

	if len(entries) == 0 {
		return sym, nil, nil
	}

	latest := entries[len(entries)-1]
	bids := make([]domain.OrderBookEntry, 0, len(latest.Bids))
	for _, b := range latest.Bids {
		if len(b) >= 2 {
			p := xjson.ToFloat64(b[0])
			v := xjson.ToFloat64(b[1])
			if p > 0 && v > 0 {
				bids = append(bids, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	asks := make([]domain.OrderBookEntry, 0, len(latest.Asks))
	for _, a := range latest.Asks {
		if len(a) >= 2 {
			p := xjson.ToFloat64(a[0])
			v := xjson.ToFloat64(a[1])
			if p > 0 && v > 0 {
				asks = append(asks, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	return sym, &domain.OrderBook{
		Symbol:  sym,
		Version: parseToobitDepthVersion(latest.V),
		Bids:    bids,
		Asks:    asks,
	}, nil
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

func (a *WsAdapter) logger() *slog.Logger {
	if a.client != nil && a.client.base != nil && a.client.base.Logger() != nil {
		return a.client.base.Logger()
	}
	return slog.Default()
}

// ParsePosition parses position update for toobit futures.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	a.logger().Debug("raw pos data", slog.String("exchange", "toobit"), slog.String("data", string(data)))
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

	log := a.logger()
	log.DebugContext(ctx, "⏳ Started keepalive loop for Toobit user stream")

	for {
		select {
		case <-ctx.Done():
			log.DebugContext(context.WithoutCancel(ctx), "⏳ Stopped keepalive loop for Toobit user stream")
			return
		case <-ticker.C:
			if a.client == nil {
				continue
			}
			if err := a.client.KeepAliveListenKey(ctx, listenKey); err != nil {
				log.ErrorContext(ctx, "🔴 Failed to keepalive Toobit user stream", slog.Any("error", err))
			} else {
				log.DebugContext(ctx, "🟢 Successfully kept alive Toobit user stream")
			}
		}
	}
}
