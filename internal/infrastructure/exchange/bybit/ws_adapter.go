package bybit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"
)

// WsAdapter implements ws.ExchangeAdapter for Bybit Futures.
type WsAdapter struct {
	pool          *pkgws.Pool
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

// SubscribeTicker subscribes to ticker push.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		"op":      wsOpSubscribe,
		wsArgsKey: []string{"tickers." + symbol},
	}
	topic := symbol + ":ticker"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from ticker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		"op":      wsOpUnsubscribe,
		wsArgsKey: []string{"tickers." + symbol},
	}
	topic := symbol + ":ticker"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeKline subscribes to 1-minute klines.
func (a *WsAdapter) SubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]any{
		"op":      wsOpSubscribe,
		wsArgsKey: []string{"kline.1." + symbol},
	}
	topic := symbol + ":kline"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeKline unsubscribes from klines.
func (a *WsAdapter) UnsubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]any{
		"op":      wsOpUnsubscribe,
		wsArgsKey: []string{"kline.1." + symbol},
	}
	topic := symbol + ":kline"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeDepth subscribes to orderbook depth.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol, step string) error {
	// Standard limit size for Bybit orderbook WS is 50 or 20
	msg := map[string]any{
		"op":      wsOpSubscribe,
		wsArgsKey: []string{"orderbook.50." + symbol},
	}
	topic := symbol + ":depth:" + step
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from orderbook depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol, step string) error {
	msg := map[string]any{
		"op":      wsOpUnsubscribe,
		wsArgsKey: []string{"orderbook.50." + symbol},
	}
	topic := symbol + ":depth:" + step
	return a.pool.UnsubscribePublic(ctx, topic, msg)
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

// GetPingConfig returns application ping and interval.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return map[string]any{
		"op": "ping",
	}, 20 * time.Second
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

		expires := a.clock.Now().UnixMilli() + 10000 // expires in 10 seconds
		reqStr := fmt.Sprintf("GET/realtime%d", expires)

		h := hmac.New(sha256.New, []byte(apiSecret))
		h.Write([]byte(reqStr))
		signature := hex.EncodeToString(h.Sum(nil))

		authMsg := map[string]any{
			"op": wsOpAuth,
			wsArgsKey: []any{
				apiKey,
				expires,
				signature,
			},
		}
		if err := client.SendJSON(authMsg); err != nil {
			slog.Error("Bybit private websocket auth send failed", slog.Any("error", err))
		}
	}
}

// GetChannelExtractor routes WebSocket push channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		var authResp struct {
			Op      string `json:"op"`
			RetCode int    `json:"retCode"`
		}
		if err := json.Unmarshal(data, &authResp); err == nil && authResp.Op == wsOpAuth {
			if authResp.RetCode == 0 {
				a.authMu.Lock()
				select {
				case <-a.authenticated:
				default:
					close(a.authenticated)
				}
				a.authMu.Unlock()
			}
		}

		var msg struct {
			Topic string `json:"topic"`
		}
		if err := json.Unmarshal(data, &msg); err == nil {
			if strings.HasPrefix(msg.Topic, "tickers.") {
				return "ticker"
			}
			if strings.HasPrefix(msg.Topic, "orderbook.") {
				return "depth"
			}
			if strings.HasPrefix(msg.Topic, "kline.") {
				return "kline"
			}
			switch msg.Topic {
			case wsTopicOrder:
				return "personal.order"
			case wsTopicPosition:
				return "personal.position"
			}
			return msg.Topic
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
	if err = json.Unmarshal(data, &msg); err != nil {
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

// ParsePosition parses push.personal.position.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg struct {
		Topic string          `json:"topic"`
		Data  []bybitPosition `json:"data"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
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
		HoldVol:         pos.HoldVol,
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
