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
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"
)

// WsAdapter implements ws.ExchangeAdapter for Bybit Futures.
type WsAdapter struct {
	pool      *pkgws.Pool
	apiKey    string
	apiSecret string
}

// NewWsAdapter creates a new Bybit WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{}
}

// SetPool injects the websocket pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// SubscribeTicker subscribes to ticker push.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		"op":      wsOpSubscribe,
		wsArgsKey: []string{"tickers." + symbol},
	}
	topic := symbol + ":ticker"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from ticker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		"op":      wsOpUnsubscribe,
		wsArgsKey: []string{"tickers." + symbol},
	}
	topic := symbol + ":ticker"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeKline subscribes to 1-minute klines.
func (a *WsAdapter) SubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		"op":      wsOpSubscribe,
		wsArgsKey: []string{"kline.1." + symbol},
	}
	topic := symbol + ":kline"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeKline unsubscribes from klines.
func (a *WsAdapter) UnsubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		"op":      wsOpUnsubscribe,
		wsArgsKey: []string{"kline.1." + symbol},
	}
	topic := symbol + ":kline"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeDepth subscribes to orderbook depth.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol, step string) error {
	// Standard limit size for Bybit orderbook WS is 50 or 20
	msg := map[string]interface{}{
		"op":      wsOpSubscribe,
		wsArgsKey: []string{"orderbook.50." + symbol},
	}
	topic := symbol + ":depth:" + step
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from orderbook depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol, step string) error {
	msg := map[string]interface{}{
		"op":      wsOpUnsubscribe,
		wsArgsKey: []string{"orderbook.50." + symbol},
	}
	topic := symbol + ":depth:" + step
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to all private futures channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	msg := map[string]interface{}{
		"op":      wsOpSubscribe,
		wsArgsKey: []string{wsTopicPosition, wsTopicOrder, "wallet"},
	}
	err := a.pool.SendPrivate(ctx, msg)
	if err != nil {
		return fmt.Errorf("bybit ws subscribe private: %w", err)
	}
	return nil
}

// GetPingConfig returns application ping and interval.
func (a *WsAdapter) GetPingConfig() (interface{}, time.Duration) {
	return map[string]interface{}{
		"op": "ping",
	}, 20 * time.Second
}

// GetAuthHook intercepts OnConnected to store credentials and authenticate private WS.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	a.apiKey = apiKey
	a.apiSecret = apiSecret

	if apiKey == "" || apiSecret == "" {
		return nil
	}

	return func(client *pkgws.Client) {
		expires := time.Now().UnixMilli() + 10000 // expires in 10 seconds
		reqStr := fmt.Sprintf("GET/realtime%d", expires)

		h := hmac.New(sha256.New, []byte(apiSecret))
		h.Write([]byte(reqStr))
		signature := hex.EncodeToString(h.Sum(nil))

		authMsg := map[string]interface{}{
			"op": "auth",
			wsArgsKey: []interface{}{
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
		Topic string        `json:"topic"`
		Data  []bybitTicker `json:"data"`
	}
	if err = json.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}
	if len(msg.Data) == 0 {
		return "", nil, fmt.Errorf("empty data in ticker push")
	}
	raw := msg.Data[0]
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

// ParseDepth parses raw JSON into exchange.OrderBook.
func (a *WsAdapter) ParseDepth(data []byte) (symbol string, ob *domain.OrderBook, err error) {
	var msg struct {
		Topic string               `json:"topic"`
		Data  bybitOrderbookResult `json:"data"`
	}
	if err = json.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}
	raw := msg.Data
	ob = &domain.OrderBook{
		Symbol: raw.Symbol,
		Asks:   make([]exchange.OrderBookEntry, 0, len(raw.Asks)),
		Bids:   make([]exchange.OrderBookEntry, 0, len(raw.Bids)),
	}
	for _, item := range raw.Asks {
		if len(item) < 2 {
			continue
		}
		p := decmath.ParseFloat(item[0])
		v := decmath.ParseFloat(item[1])
		if p > 0 {
			ob.Asks = append(ob.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}
	for _, item := range raw.Bids {
		if len(item) < 2 {
			continue
		}
		p := decmath.ParseFloat(item[0])
		v := decmath.ParseFloat(item[1])
		if p > 0 {
			ob.Bids = append(ob.Bids, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}
	return raw.Symbol, ob, nil
}

// ParseKline parses raw JSON into exchange.Kline.
func (a *WsAdapter) ParseKline(data []byte) (symbol string, k *exchange.Kline, err error) {
	var msg struct {
		Topic string `json:"topic"`
		Data  []struct {
			Start  int64  `json:"start"`
			Open   string `json:"open"`
			Close  string `json:"close"`
			High   string `json:"high"`
			Low    string `json:"low"`
			Volume string `json:"volume"`
		} `json:"data"`
	}
	if err = json.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}
	if len(msg.Data) == 0 {
		return "", nil, fmt.Errorf("empty data in kline push")
	}
	raw := msg.Data[0]

	// Extract symbol from topic (e.g. kline.1.BTCUSDT -> BTCUSDT)
	parts := strings.Split(msg.Topic, ".")
	if len(parts) >= 3 {
		symbol = parts[2]
	}

	k = &exchange.Kline{
		Timestamp: raw.Start,
		Open:      decmath.ParseFloat(raw.Open),
		Close:     decmath.ParseFloat(raw.Close),
		High:      decmath.ParseFloat(raw.High),
		Low:       decmath.ParseFloat(raw.Low),
		Volume:    decmath.ParseFloat(raw.Volume),
	}
	return symbol, k, nil
}

// ParseOrder parses raw JSON into exchange.WsOrderDeal.
func (a *WsAdapter) ParseOrder(data []byte) (*exchange.WsOrderDeal, error) {
	var msg struct {
		Topic string       `json:"topic"`
		Data  []bybitOrder `json:"data"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	if len(msg.Data) == 0 {
		return nil, fmt.Errorf("empty data in order push")
	}
	raw := msg.Data[0]
	deal := mapOrderInfo(raw)

	wsDeal := &exchange.WsOrderDeal{
		Symbol:       deal.Symbol,
		OrderID:      deal.OrderID,
		Price:        deal.Price,
		Vol:          deal.Vol,
		DealVol:      deal.DealVol,
		DealAvgPrice: deal.DealAvgPrice,
		State:        deal.State,
		ExternalOID:  deal.ExternalOID,
		Side:         deal.Side,
		PositionMode: deal.PositionMode,
	}

	return wsDeal, nil
}

// ParseOrderDeal is stubbed since we use WsOrderDeal for routing.
func (a *WsAdapter) ParseOrderDeal(data []byte) (*exchange.PersonalOrderDeal, error) {
	return nil, nil
}

// ParseTrackOrder is stubbed.
func (a *WsAdapter) ParseTrackOrder(data []byte) (*exchange.PersonalTrackOrderUpdate, error) {
	return nil, nil
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
	raw := msg.Data[0]
	pos := mapPosition(raw)

	update := &exchange.PersonalPositionUpdate{
		Symbol:       pos.Symbol,
		HoldVol:      pos.HoldVol,
		HoldAvgPrice: pos.HoldAvgPrice,
		OpenAvgPrice: pos.OpenAvgPrice,
		Leverage:     pos.Leverage,
		Realized:     pos.Realised,
		PositionType: pos.PositionType,
	}

	return update, nil
}
