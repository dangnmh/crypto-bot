package mexc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"
)

// WsAdapter implements ws.ExchangeAdapter for MEXC Futures.
type WsAdapter struct {
	pool *pkgws.Pool
}

// NewWsAdapter creates a new MEXC WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{}
}

// SetPool injects the websocket pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// SubscribeTicker subscribes to ticker push.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: "sub.ticker",
		paramParam:  map[string]string{paramSymbol: symbol},
	}
	topic := symbol + ":" + channelTicker
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from ticker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: "unsub.ticker",
		paramParam:  map[string]string{paramSymbol: symbol},
	}
	topic := symbol + ":" + channelTicker
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeKline subscribes to 1-minute klines.
func (a *WsAdapter) SubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: "sub.kline",
		paramParam:  map[string]string{paramSymbol: symbol, paramInterval: "Min1"},
	}
	topic := symbol + ":" + channelKline
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeKline unsubscribes from klines.
func (a *WsAdapter) UnsubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: "unsub.kline",
		paramParam:  map[string]string{paramSymbol: symbol},
	}
	topic := symbol + ":" + channelKline
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to all private futures channels used by funding flows.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	msg := map[string]any{
		paramMethod: "personal.filter",
		paramParam: map[string]any{
			"filters": []map[string]string{
				{paramFilter: "order"},
				{paramFilter: "order.deal"},
				{paramFilter: "track.order"},
				{paramFilter: "position"},
			},
		},
	}
	return a.pool.SendPrivate(ctx, msg)
}

// SubscribeDepth subscribes to orderbook depth.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol, step string) error {
	method := "sub.depth.full"
	param := map[string]any{
		paramSymbol: symbol,
		paramLimit:  20,
	}
	if step != "" {
		method = "sub.depth.step"
		param["step"] = step
	}
	msg := map[string]any{
		paramMethod: method,
		paramParam:  param,
	}
	topic := symbol + ":depth:" + step
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from orderbook depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol, step string) error {
	method := "unsub.depth.full"
	param := map[string]any{
		paramSymbol: symbol,
		paramLimit:  20,
	}
	if step != "" {
		method = "unsub.depth.step"
		param["step"] = step
	}
	msg := map[string]any{
		paramMethod: method,
		paramParam:  param,
	}
	topic := symbol + ":depth:" + step
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// GetPingConfig returns the ping payload and interval for MEXC.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return map[string]string{paramMethod: "ping"}, 15 * time.Second
}

// GetAuthHook returns the OnConnected hook for MEXC authentication.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	if apiKey == "" {
		return nil
	}
	return func(c *pkgws.Client) {
		reqTime := fmt.Sprintf("%d", time.Now().UnixMilli())
		message := apiKey + reqTime
		mac := hmac.New(sha256.New, []byte(apiSecret))
		mac.Write([]byte(message))
		signature := hex.EncodeToString(mac.Sum(nil))

		msg := map[string]any{
			paramMethod: "login",
			paramParam: map[string]any{
				"apiKey":    apiKey,
				"reqTime":   reqTime,
				"signature": signature,
				"subscribe": false,
			},
		}
		_ = c.SendJSON(msg)
	}
}

// GetChannelExtractor returns the function that maps raw MEXC messages to generic internal channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		var baseMsg struct {
			Channel string `json:"channel"`
		}
		if err := json.Unmarshal(data, &baseMsg); err == nil {
			switch baseMsg.Channel {
			case "push.ticker":
				return channelTicker
			case "push.depth.full", "push.depth.step":
				return channelDepth
			case "push.kline":
				return channelKline
			case "push.personal.order":
				return "personal.order"
			case "push.personal.order.deal":
				return "personal.order.deal"
			case "push.personal.track.order":
				return "personal.track.order"
			case "push.personal.position":
				return "personal.position"
			default:
				if after, ok := strings.CutPrefix(baseMsg.Channel, "push."); ok {
					return after
				}
				return baseMsg.Channel
			}
		}
		return ""
	}
}

// WsTickerData represents MEXC's ticker format.
type WsTickerData struct {
	Symbol      string  `json:"symbol"`
	LastPrice   float64 `json:"lastPrice"`
	FairPrice   float64 `json:"fairPrice"`
	IndexPrice  float64 `json:"indexPrice"`
	Volume24    float64 `json:"volume24"`
	Amount24    float64 `json:"amount24"`
	MaxBidPrice float64 `json:"maxBidPrice"`
	MinAskPrice float64 `json:"minAskPrice"`
	Timestamp   int64   `json:"timestamp"`
	Bid1        float64 `json:"bid1"`
	Ask1        float64 `json:"ask1"`
}

// ParseTicker parses raw JSON into generic store.PriceData.
func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	var msg struct {
		Symbol string          `json:"symbol"`
		Data   json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}

	var ticker WsTickerData
	if err = json.Unmarshal(msg.Data, &ticker); err != nil {
		return "", nil, err
	}

	pd = &store.PriceData{
		Symbol:    msg.Symbol,
		LastPrice: ticker.LastPrice,
		BestBid:   ticker.Bid1,
		BestAsk:   ticker.Ask1,
		FairPrice: ticker.FairPrice,
		Volume24:  ticker.Volume24,
		UpdatedAt: time.Now(),
	}

	if pd.BestBid == 0 && ticker.MaxBidPrice > 0 {
		pd.BestBid = ticker.MaxBidPrice
	}
	if pd.BestAsk == 0 && ticker.MinAskPrice > 0 {
		pd.BestAsk = ticker.MinAskPrice
	}

	return msg.Symbol, pd, nil
}

// ParsePosition parses push.personal.position into position exposure data.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	type updateAlias exchange.PersonalPositionUpdate
	var raw struct {
		updateAlias
		PositionID json.RawMessage `json:"positionId"`
	}

	if err := json.Unmarshal(msg.Data, &raw); err != nil {
		return nil, err
	}

	update := exchange.PersonalPositionUpdate(raw.updateAlias)

	return &update, nil
}
