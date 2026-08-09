package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"

	"crypto-bot/pkg/xjson"
)

// WsAdapter implements ws.ExchangeAdapter for Binance Futures.
type WsAdapter struct {
	pool         *pkgws.Pool
	client       *Client
	apiKey       string
	apiSecret    string
	cancelKeep   context.CancelFunc
	cancelKeepMu sync.Mutex
	privateURL   string
	publicURL    string
	marketURL    string
}

// NewWsAdapter creates a new Binance WsAdapter with the configured private URL.
func NewWsAdapter(privateURL string) *WsAdapter {
	return &WsAdapter{
		privateURL: privateURL,
	}
}

// SetURLs sets the public and market URLs.
func (a *WsAdapter) SetURLs(publicURL, marketURL string) {
	a.publicURL = publicURL
	a.marketURL = marketURL
}

// SetPool injects the websocket pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// SetClient injects the REST client reference.
func (a *WsAdapter) SetClient(client *Client) {
	a.client = client
}

// SubscribeTicker subscribes to ticker push.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	bookMsg := map[string]any{
		paramMethod: opSubscribe,
		paramParams: []string{
			strings.ToLower(symbol) + "@bookTicker",
		},
		"id": time.Now().UnixMilli(),
	}
	pubURL := a.publicURL
	if pubURL == "" {
		pubURL = defaultPublicURL
	}
	bookTopic := symbol + ":ticker:book"
	if err := a.pool.SubscribePublicWithURL(ctx, pubURL, bookTopic, bookMsg); err != nil {
		return err
	}

	marketMsg := map[string]any{
		paramMethod: opSubscribe,
		paramParams: []string{
			strings.ToLower(symbol) + "@miniTicker",
			strings.ToLower(symbol) + "@ticker",
		},
		"id": time.Now().UnixMilli() + 1,
	}
	mktURL := a.marketURL
	if mktURL == "" {
		mktURL = defaultMarketURL
	}
	marketTopic := symbol + ":ticker:market"
	if err := a.pool.SubscribePublicWithURL(ctx, mktURL, marketTopic, marketMsg); err != nil {
		_ = a.pool.UnsubscribePublicWithURL(ctx, bookTopic, map[string]any{
			paramMethod: opUnsubscribe,
			paramParams: []string{strings.ToLower(symbol) + "@bookTicker"},
			"id":        time.Now().UnixMilli(),
		})
		return err
	}

	return nil
}

// UnsubscribeTicker unsubscribes from ticker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	bookMsg := map[string]any{
		paramMethod: opUnsubscribe,
		paramParams: []string{
			strings.ToLower(symbol) + "@bookTicker",
		},
		"id": time.Now().UnixMilli(),
	}
	bookTopic := symbol + ":ticker:book"
	err1 := a.pool.UnsubscribePublicWithURL(ctx, bookTopic, bookMsg)

	marketMsg := map[string]any{
		paramMethod: opUnsubscribe,
		paramParams: []string{
			strings.ToLower(symbol) + "@miniTicker",
			strings.ToLower(symbol) + "@ticker",
		},
		"id": time.Now().UnixMilli() + 1,
	}
	marketTopic := symbol + ":ticker:market"
	err2 := a.pool.UnsubscribePublicWithURL(ctx, marketTopic, marketMsg)

	if err1 != nil {
		return err1
	}
	return err2
}

// SubscribePersonal subscribes to all private futures channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	// For Binance User Data stream, we listen directly to the stream established by listenKey.
	// Since no explicit channel SUBSCRIBE frames are required on user stream connection, we stub this safely.
	return nil
}

// GetPingConfig returns application ping and interval.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return nil, 0
}

// GetAuthHook intercepts OnConnected to store credentials and authenticate.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	a.apiKey = apiKey
	a.apiSecret = apiSecret
	return nil
}

// GetChannelExtractor routes WebSocket push channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return parseStandardWS
}

func parseStandardWS(data []byte) string {
	var msg struct {
		EventName string `json:"e"`
		EventTime int64  `json:"E"`
		Stream    string `json:"stream"`
	}
	if err := xjson.Unmarshal(data, &msg); err != nil {
		slog.Error("parseStandardWS error", slog.Any("error", err), slog.String("data", string(data)))
		return ""
	}

	// 1. Check by stream suffix first
	if msg.Stream != "" {
		if strings.HasSuffix(msg.Stream, "@ticker") || strings.HasSuffix(msg.Stream, "@bookTicker") || strings.HasSuffix(msg.Stream, "@miniTicker") {
			return chTicker
		}
		if strings.HasSuffix(msg.Stream, "@depth20@100ms") {
			return chDepth
		}
		if strings.HasSuffix(msg.Stream, "@kline_1m") {
			return chKline
		}
	}

	// 2. Fallback to event name
	switch msg.EventName {
	case evt24hrTicker, evtBookTicker, evt24hrMiniTicker:
		return chTicker
	case "depthUpdate":
		return chDepth
	case msgKline:
		return chKline
	case "ORDER_TRADE_UPDATE":
		return chOrder
	case "ACCOUNT_UPDATE":
		return chPosition
	default:
		return msg.EventName
	}
}

// ParseTicker parses raw JSON into generic store.PriceData.
func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	// 1. Try parsing standard stream wrapper format (e.g. {"stream": "...", "data": {...}})
	var wrap struct {
		Stream string          `json:"stream"`
		Data   json.RawMessage `json:"data"`
	}
	rawJSON := data
	if errWrap := xjson.Unmarshal(data, &wrap); errWrap == nil && len(wrap.Data) > 0 {
		rawJSON = wrap.Data
	}

	// 2. Extract event type to route specialized payloads
	var eventType struct {
		EventName string `json:"e"`
	}
	_ = xjson.Unmarshal(rawJSON, &eventType)

	switch eventType.EventName {
	case evt24hrTicker:
		return parse24hTickerEvent(rawJSON)
	case evt24hrMiniTicker:
		return parse24hMiniTickerEvent(rawJSON)
	case evtBookTicker:
		return parseBookTickerEvent(rawJSON)
	default:
		return parseFallbackTicker(rawJSON)
	}
}

func parse24hTickerEvent(rawJSON []byte) (string, *store.PriceData, error) {
	var raw struct {
		Symbol    string `json:"s"`
		LastPrice string `json:"c"`
		CloseTime int64  `json:"C"` // Explicitly declared to prevent case-insensitive "C" -> "c" type mismatch collision
		Volume24  string `json:"v"`
	}
	if errRaw := xjson.Unmarshal(rawJSON, &raw); errRaw != nil || raw.Symbol == "" {
		return "", nil, fmt.Errorf("invalid ticker event payload: %w", errRaw)
	}
	pd := &store.PriceData{
		Symbol:    raw.Symbol,
		LastPrice: decmath.ParseFloat(raw.LastPrice),
		FairPrice: decmath.ParseFloat(raw.LastPrice),
		Volume24:  decmath.ParseFloat(raw.Volume24),
		UpdatedAt: time.Now(),
	}
	return raw.Symbol, pd, nil
}

func parse24hMiniTickerEvent(rawJSON []byte) (string, *store.PriceData, error) {
	var raw struct {
		Symbol    string `json:"s"`
		LastPrice string `json:"c"`
		Volume24  string `json:"v"`
	}
	if errRaw := xjson.Unmarshal(rawJSON, &raw); errRaw != nil || raw.Symbol == "" {
		return "", nil, fmt.Errorf("invalid miniTicker event payload: %w", errRaw)
	}
	pd := &store.PriceData{
		Symbol:    raw.Symbol,
		LastPrice: decmath.ParseFloat(raw.LastPrice),
		FairPrice: decmath.ParseFloat(raw.LastPrice),
		Volume24:  decmath.ParseFloat(raw.Volume24),
		UpdatedAt: time.Now(),
	}
	return raw.Symbol, pd, nil
}

func parseBookTickerEvent(rawJSON []byte) (string, *store.PriceData, error) {
	var raw struct {
		Symbol     string `json:"s"`
		BestBid    string `json:"b"`
		BestBidQty string `json:"B"` // Prevent case-insensitive "B" -> "b" type mismatch
		BestAsk    string `json:"a"`
		BestAskQty string `json:"A"` // Prevent case-insensitive "A" -> "a" type mismatch
	}
	if errRaw := xjson.Unmarshal(rawJSON, &raw); errRaw != nil || raw.Symbol == "" {
		return "", nil, fmt.Errorf("invalid bookTicker event payload: %w", errRaw)
	}
	bid := decmath.ParseFloat(raw.BestBid)
	ask := decmath.ParseFloat(raw.BestAsk)
	mid := 0.0
	if bid > 0 && ask > 0 {
		mid = (bid + ask) / 2.0
	}
	pd := &store.PriceData{
		Symbol:    raw.Symbol,
		BestBid:   bid,
		BestAsk:   ask,
		LastPrice: mid,
		FairPrice: mid,
		UpdatedAt: time.Now(),
	}
	return raw.Symbol, pd, nil
}

func parseFallbackTicker(rawJSON []byte) (string, *store.PriceData, error) {
	var raw struct {
		Symbol    string `json:"s"`
		LastPrice string `json:"c"`
		BestBid   string `json:"b"`
		BestAsk   string `json:"a"`
		Volume24  string `json:"v"`
	}
	if errRaw := xjson.Unmarshal(rawJSON, &raw); errRaw != nil || raw.Symbol == "" {
		return "", nil, fmt.Errorf("invalid fallback ticker payload: %w", errRaw)
	}
	bid := decmath.ParseFloat(raw.BestBid)
	ask := decmath.ParseFloat(raw.BestAsk)
	last := decmath.ParseFloat(raw.LastPrice)
	if last == 0 && bid > 0 && ask > 0 {
		last = (bid + ask) / 2.0
	}
	pd := &store.PriceData{
		Symbol:    raw.Symbol,
		LastPrice: last,
		BestBid:   bid,
		BestAsk:   ask,
		FairPrice: last,
		Volume24:  decmath.ParseFloat(raw.Volume24),
		UpdatedAt: time.Now(),
	}
	return raw.Symbol, pd, nil
}

// ParsePosition parses push.personal.position.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg struct {
		Update struct {
			Positions []struct {
				Symbol       string `json:"s"`
				Amount       string `json:"pa"`
				EntryPrice   string `json:"ep"`
				Unrealized   string `json:"up"`
				PositionSide string `json:"ps"`
			} `json:"P"`
		} `json:"a"`
	}
	if err := xjson.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	if len(msg.Update.Positions) == 0 {
		return nil, fmt.Errorf("empty position update in push")
	}

	var raw = msg.Update.Positions[0]
	for _, item := range msg.Update.Positions {
		if math.Abs(decmath.ParseFloat(item.Amount)) > 0 {
			raw = item
			break
		}
	}

	amt := decmath.ParseFloat(raw.Amount)
	posType := exchange.PositionTypeLong
	if amt < 0 || raw.PositionSide == posSideShort {
		posType = exchange.PositionTypeShort
	}

	update := &exchange.PersonalPositionUpdate{
		Symbol:          raw.Symbol,
		HoldVolCoin:     math.Abs(amt),
		HoldAvgPrice:    decmath.ParseFloat(raw.EntryPrice),
		CloseProfitLoss: decmath.ParseFloat(raw.Unrealized),
		PositionType:    posType,
	}

	return update, nil
}

// GetPrivateURLFunc returns a dynamic URL generation function for the private WebSocket connection.
func (a *WsAdapter) GetPrivateURLFunc(ctx context.Context) func() (string, error) {
	return func() (string, error) {
		if a.client == nil {
			return "", fmt.Errorf("binance REST client not injected in WsAdapter")
		}

		// 1. Fetch new listenKey via REST API
		listenKey, err := a.client.CreateListenKey(ctx)
		if err != nil {
			return "", fmt.Errorf("create binance listen key failed: %w", err)
		}

		// 2. Prevent goroutine leaks by canceling any previous keepalive loop
		a.cancelKeepMu.Lock()
		if a.cancelKeep != nil {
			a.cancelKeep()
		}
		keepCtx, cancel := context.WithCancel(ctx)
		a.cancelKeep = cancel
		a.cancelKeepMu.Unlock()

		// 3. Start keepalive loop for the new listenKey
		go a.keepAliveLoop(keepCtx)

		// 4. Construct private websocket URL.
		base := a.privateURL
		if base == "" {
			base = "wss://fstream.binance.com/private/ws"
		}

		base = strings.TrimSuffix(base, "/")
		return base + "/" + listenKey, nil
	}
}

func (a *WsAdapter) keepAliveLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	a.client.logger.DebugContext(ctx, "⏳ Started keepalive loop for Binance user data stream")

	for {
		select {
		case <-ctx.Done():
			a.client.logger.DebugContext(context.WithoutCancel(ctx), "⏳ Stopped keepalive loop for Binance user data stream")
			return
		case <-ticker.C:
			if err := a.client.KeepAliveListenKey(ctx); err != nil {
				a.client.logger.ErrorContext(ctx, "🔴 Failed to keepalive Binance user data stream", slog.Any("error", err))
			} else {
				a.client.logger.DebugContext(ctx, "🟢 Successfully kept alive Binance user data stream")
			}
		}
	}
}
