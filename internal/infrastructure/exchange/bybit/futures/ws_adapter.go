package futures

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"
)

var (
	_ exchange.DepthSubscriber = (*WsAdapter)(nil)
	_ exchange.DepthParser     = (*WsAdapter)(nil)
	_ exchange.TradeSubscriber = (*WsAdapter)(nil)
	_ exchange.TradeParser     = (*WsAdapter)(nil)
)

// WsAdapter implements ws.ExchangeAdapter for Bybit Futures.
type WsAdapter struct {
	pool          *pkgws.Pool
	client        *Client
	apiKey        string
	apiSecret     string
	clock         exchange.Clock
	authenticated chan struct{}
	authMu        sync.Mutex
}

// NewWsAdapter creates a new Bybit WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{
		clock:         exchange.RealClock{},
		authenticated: make(chan struct{}),
	}
}

// SetClient injects the REST client reference.
func (a *WsAdapter) SetClient(client *Client) {
	a.client = client
}

// SetClock configures a custom clock implementation.
func (a *WsAdapter) SetClock(clk exchange.Clock) {
	if clk != nil {
		a.clock = clk
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

// SubscribeTicker subscribes to ticker push.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		"op":      wsOpSubscribe,
		wsArgsKey: []string{"tickers." + symbol},
	}
	topic := symbol + ":ticker"
	return a.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from ticker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		"op":      wsOpUnsubscribe,
		wsArgsKey: []string{"tickers." + symbol},
	}
	topic := symbol + ":ticker"
	return a.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeTrade subscribes to real-time public trade deals.
func (a *WsAdapter) SubscribeTrade(ctx context.Context, symbol string) error {
	msg := map[string]any{
		"op":      wsOpSubscribe,
		wsArgsKey: []string{"publicTrade." + symbol},
	}
	topic := symbol + ":trade"
	return a.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTrade stops subscribing to real-time public trade deals.
func (a *WsAdapter) UnsubscribeTrade(ctx context.Context, symbol string) error {
	msg := map[string]any{
		"op":      wsOpUnsubscribe,
		wsArgsKey: []string{"publicTrade." + symbol},
	}
	topic := symbol + ":trade"
	return a.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeDepth streams orderbook depth updates.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol string) error {
	msg := map[string]any{
		"op":      wsOpSubscribe,
		wsArgsKey: []string{"orderbook.50." + symbol},
	}
	topic := symbol + ":depth"
	return a.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth stops streaming orderbook depth updates.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol string) error {
	msg := map[string]any{
		"op":      wsOpUnsubscribe,
		wsArgsKey: []string{"orderbook.50." + symbol},
	}
	topic := symbol + ":depth"
	return a.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to all private futures channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	a.authMu.Lock()
	authCh := a.authenticated
	a.authMu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-authCh:
	}

	msg := map[string]any{
		"op":      wsOpSubscribe,
		wsArgsKey: []string{wsTopicPosition},
	}
	err := a.pool.SendPrivate(ctx, msg)
	if err != nil {
		return fmt.Errorf("bybit ws subscribe private: %w", err)
	}
	return nil
}

func (a *WsAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
}

// GetPingConfig returns application ping and interval.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return GetBybitPingConfig()
}

// GetPongDetector returns the pong frame matcher for Bybit V5.
func (a *WsAdapter) GetPongDetector() func([]byte) bool {
	return IsBybitPong
}

// GetAuthHook intercepts OnConnected to store credentials and authenticate private WS.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	a.apiKey = apiKey
	a.apiSecret = apiSecret

	if apiKey == "" || apiSecret == "" {
		a.authMu.Lock()
		select {
		case <-a.authenticated:
		default:
			close(a.authenticated)
		}
		a.authMu.Unlock()
		return nil
	}

	return func(client *pkgws.Client) {
		a.authMu.Lock()
		a.authenticated = make(chan struct{})
		a.authMu.Unlock()

		authMsg := BuildBybitAuthMessage(apiKey, apiSecret, a.clock.Now().UnixMilli())
		if err := client.SendJSON(authMsg); err != nil {
			slog.Error("Bybit private websocket auth send failed", slog.Any("error", err))
		}
	}
}

func (a *WsAdapter) handleAuthResponse(data []byte) {
	if _, success, _, _, _ := ParseBybitAuthResponse(data); success {
		a.authMu.Lock()
		select {
		case <-a.authenticated:
		default:
			close(a.authenticated)
		}
		a.authMu.Unlock()
	}
}

func routeBybitTopic(topic string) string {
	switch {
	case strings.HasPrefix(topic, "tickers."):
		return "ticker"
	case strings.HasPrefix(topic, "publicTrade."):
		return "trade"
	case strings.HasPrefix(topic, "orderbook."):
		return "depth"
	case strings.HasPrefix(topic, "kline."):
		return "kline"
	case topic == wsTopicOrder:
		return "personal.order"
	case topic == wsTopicPosition:
		return "personal.position"
	default:
		return topic
	}
}

// GetChannelExtractor routes WebSocket push channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		a.handleAuthResponse(data)

		var msg struct {
			Topic string `json:"topic"`
		}
		if err := xjson.Unmarshal(data, &msg); err == nil {
			return routeBybitTopic(msg.Topic)
		}
		return ""
	}
}

// ParseTicker parses raw JSON into generic store.PriceData.
func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	var msg struct {
		Topic string       `json:"topic"`
		Data  *bybitTicker `json:"data"`
	}
	if err = xjson.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}
	if msg.Data == nil {
		return "", nil, fmt.Errorf("empty data in ticker push")
	}

	raw := msg.Data
	pd = &store.PriceData{
		Symbol:    raw.Symbol,
		LastPrice: decmath.ParseFloat(raw.LastPrice),
		BestBid:   decmath.ParseFloat(raw.Bid1Price),
		BestAsk:   decmath.ParseFloat(raw.Ask1Price),
		Volume24:  decmath.ParseFloat(raw.Volume24h),
		UpdatedAt: time.Now(),
	}
	return raw.Symbol, pd, nil
}

type wsTradeEntry struct {
	T    int64  `json:"T"`
	S    string `json:"s"`
	Side string `json:"S"` // "Buy", "Sell"
	V    string `json:"v"`
	P    string `json:"p"`
	L    string `json:"L"`
	I    string `json:"i"`
	BT   bool   `json:"BT"`
	Seq  int64  `json:"seq"`
}

func parseTradeEntries(raw json.RawMessage) ([]wsTradeEntry, error) {
	var entries []wsTradeEntry
	if err := xjson.Unmarshal(raw, &entries); err == nil {
		return entries, nil
	}
	var single wsTradeEntry
	if err := xjson.Unmarshal(raw, &single); err == nil {
		return []wsTradeEntry{single}, nil
	}
	return nil, fmt.Errorf("unmarshal trade data failed")
}

func mapTradeEntry(entry wsTradeEntry, fallbackSym string, fallbackTs int64) *domain.PublicTrade {
	price := decmath.ParseFloat(entry.P)
	volume := decmath.ParseFloat(entry.V)
	if price <= 0 || volume <= 0 {
		return nil
	}

	itemSym := entry.S
	if itemSym == "" {
		itemSym = fallbackSym
	}

	side := domain.SideOpenLong
	if strings.EqualFold(entry.Side, "sell") {
		side = domain.SideOpenShort
	}

	tradeTime := time.Now().UTC()
	if entry.T > 0 {
		tradeTime = time.UnixMilli(entry.T).UTC()
	} else if fallbackTs > 0 {
		tradeTime = time.UnixMilli(fallbackTs).UTC()
	}

	return &domain.PublicTrade{
		Symbol:    itemSym,
		Price:     price,
		Volume:    volume,
		Side:      side,
		Timestamp: tradeTime,
	}
}

// ParseTrade parses public trade messages into []domain.PublicTrade.
func (a *WsAdapter) ParseTrade(data []byte) (string, []domain.PublicTrade, error) {
	var push struct {
		Topic string          `json:"topic"`
		Type  string          `json:"type"`
		Ts    int64           `json:"ts"`
		Data  json.RawMessage `json:"data"`
	}
	if err := xjson.Unmarshal(data, &push); err != nil {
		return "", nil, fmt.Errorf("unmarshal trade push: %w", err)
	}

	sym := strings.TrimPrefix(push.Topic, "publicTrade.")
	if len(push.Data) == 0 || string(push.Data) == "null" {
		return sym, nil, nil
	}

	entries, err := parseTradeEntries(push.Data)
	if err != nil {
		return "", nil, err
	}
	if len(entries) == 0 {
		return sym, nil, nil
	}
	if sym == "" {
		sym = entries[0].S
	}

	trades := make([]domain.PublicTrade, 0, len(entries))
	for _, entry := range entries {
		if pt := mapTradeEntry(entry, sym, push.Ts); pt != nil {
			trades = append(trades, *pt)
		}
	}

	return sym, trades, nil
}

// ParsePosition parses push.personal.position.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg struct {
		Topic string          `json:"topic"`
		Data  []bybitPosition `json:"data"`
	}
	if err := xjson.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	if len(msg.Data) == 0 {
		return nil, fmt.Errorf("empty data in position push")
	}
	raw := selectPositionUpdate(msg.Data)
	pos := mapPosition(raw)

	realisedPnl := raw.CurRealisedPnl
	if realisedPnl == "" {
		realisedPnl = raw.CumRealisedPnl
	}
	if realisedPnl == "" {
		realisedPnl = raw.UnrealisedPnl
	}

	update := &exchange.PersonalPositionUpdate{
		Symbol:          pos.Symbol,
		HoldVolCoin:     pos.HoldVolCoin,
		HoldAvgPrice:    pos.HoldAvgPrice,
		OpenAvgPrice:    pos.OpenAvgPrice,
		Leverage:        decmath.ParseInt(raw.Leverage),
		CloseProfitLoss: decmath.ParseFloat(realisedPnl),
		PositionType:    pos.PositionType,
		LiquidatePrice:  decmath.ParseFloat(raw.LiqPrice),
		UpdateTime:      decmath.ParseInt64(raw.UpdatedTime),
	}

	return update, nil
}

func selectPositionUpdate(positions []bybitPosition) bybitPosition {
	// 1. If any position has active size > 0, pick it first
	for i := range positions {
		if decmath.ParseFloat(positions[i].Size) > 0 {
			return positions[i]
		}
	}

	// 2. Otherwise, select the one that was most recently updated
	bestIdx := 0
	maxTime := int64(0)
	for i := range positions {
		t := int64(decmath.ParseFloat(positions[i].UpdatedTime))
		if t > maxTime {
			maxTime = t
			bestIdx = i
		}
	}
	return positions[bestIdx]
}

// ParseDepth parses depth messages into domain.OrderBook.
func (a *WsAdapter) ParseDepth(data []byte) (string, *domain.OrderBook, error) {
	return "", nil, nil
}
