package gate

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"
)

// WsAdapter implements ws.ExchangeAdapter for Gate.io Futures.
type WsAdapter struct {
	pool      *pkgws.Pool
	apiKey    string
	apiSecret string
}

// NewWsAdapter creates a new Gate.io WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{}
}

// SetPool injects the websocket pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// sign generates HMAC-SHA512 signature for subscribing to private channels.
func (a *WsAdapter) sign(channel, event string, timestamp int64) string {
	message := fmt.Sprintf("channel=%s&event=%s&time=%d", channel, event, timestamp)
	mac := hmac.New(sha512.New, []byte(a.apiSecret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// SubscribeTicker subscribes to ticker push.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	unixSec := time.Now().Unix()
	msg := map[string]any{
		gateJSONTime:    unixSec,
		gateJSONChannel: gateChannelTickers,
		gateJSONEvent:   gateEventSubscribe,
		gateJSONPayload: []string{symbol},
	}
	topic := symbol + ":ticker"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from ticker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	unixSec := time.Now().Unix()
	msg := map[string]any{
		gateJSONTime:    unixSec,
		gateJSONChannel: gateChannelTickers,
		gateJSONEvent:   gateEventUnsubscribe,
		gateJSONPayload: []string{symbol},
	}
	topic := symbol + ":ticker"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeKline subscribes to 1-minute klines.
func (a *WsAdapter) SubscribeKline(ctx context.Context, symbol string) error {
	unixSec := time.Now().Unix()
	msg := map[string]any{
		gateJSONTime:    unixSec,
		gateJSONChannel: gateChannelCandlesticks,
		gateJSONEvent:   gateEventSubscribe,
		gateJSONPayload: []string{"1m", symbol},
	}
	topic := symbol + ":kline"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeKline unsubscribes from klines.
func (a *WsAdapter) UnsubscribeKline(ctx context.Context, symbol string) error {
	unixSec := time.Now().Unix()
	msg := map[string]any{
		gateJSONTime:    unixSec,
		gateJSONChannel: gateChannelCandlesticks,
		gateJSONEvent:   gateEventUnsubscribe,
		gateJSONPayload: []string{"1m", symbol},
	}
	topic := symbol + ":kline"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeDepth subscribes to orderbook depth.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol, step string) error {
	unixSec := time.Now().Unix()
	msg := map[string]any{
		gateJSONTime:    unixSec,
		gateJSONChannel: gateChannelOrderBook,
		gateJSONEvent:   gateEventSubscribe,
		gateJSONPayload: []string{symbol, "20", "0"}, // symbol, depth, interval in ms ("0" for real-time)
	}
	topic := symbol + ":depth:" + step
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from orderbook depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol, step string) error {
	unixSec := time.Now().Unix()
	msg := map[string]any{
		gateJSONTime:    unixSec,
		gateJSONChannel: gateChannelOrderBook,
		gateJSONEvent:   gateEventUnsubscribe,
		gateJSONPayload: []string{symbol, "20", "0"},
	}
	topic := symbol + ":depth:" + step
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to all private futures channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	unixSec := time.Now().Unix()

	// 1. Subscribe to Positions channel
	posSign := a.sign(gateChannelPositions, gateEventSubscribe, unixSec)
	posMsg := map[string]any{
		gateJSONTime:    unixSec,
		gateJSONChannel: gateChannelPositions,
		gateJSONEvent:   gateEventSubscribe,
		gateJSONAuth: map[string]string{
			gateJSONMethod: gateAuthMethodAPIKey,
			gateJSONKey:    a.apiKey,
			gateJSONSign:   posSign,
		},
		gateJSONPayload: []string{gatePayloadAll},
	}
	err := a.pool.SendPrivate(ctx, posMsg)
	if err != nil {
		return fmt.Errorf("gate.io ws subscribe positions: %w", err)
	}

	return nil
}

// GetPingConfig returns application ping and interval.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	// Gate uses standard ping but some channels support active pings.
	unixSec := time.Now().Unix()
	return map[string]any{
		gateJSONTime:    unixSec,
		gateJSONChannel: gateChannelPing,
	}, 15 * time.Second
}

// GetAuthHook intercepts OnConnected to store credentials and authenticate.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	a.apiKey = apiKey
	a.apiSecret = apiSecret
	// Credentials stored, subscriptions are authenticated individually in SubscribePersonal.
	return nil
}

// GetChannelExtractor routes WebSocket push channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		var msg struct {
			Channel string `json:"channel"`
		}
		if err := json.Unmarshal(data, &msg); err == nil {
			switch msg.Channel {
			case gateChannelTickers:
				return "ticker"
			case gateChannelOrderBook:
				return "depth"
			case gateChannelCandlesticks:
				return "kline"
			case gateChannelOrders:
				return "personal.order"
			case gateChannelPositions:
				return "personal.position"
			}
			return msg.Channel
		}
		return ""
	}
}

// ParseTicker parses raw JSON into generic store.PriceData.
func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	var msg struct {
		Result struct {
			Contract string      `json:"s"` // contract
			Bid      json.Number `json:"b"` // best bid
			BidSize  json.Number `json:"B"` // best bid size
			Ask      json.Number `json:"a"` // best ask
			AskSize  json.Number `json:"A"` // best ask size
		} `json:"result"`
	}
	if err = json.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}
	if msg.Result.Contract == "" {
		return "", nil, nil
	}

	bid, _ := msg.Result.Bid.Float64()
	ask, _ := msg.Result.Ask.Float64()
	pd = &store.PriceData{
		Symbol:    msg.Result.Contract,
		BestBid:   bid,
		BestAsk:   ask,
		LastPrice: 0.5 * (bid + ask), // fallback mid market price
		UpdatedAt: time.Now(),
	}
	return msg.Result.Contract, pd, nil
}

// ParsePosition parses push.personal.position.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var eventMsg struct {
		Event string `json:"event"`
	}
	if err := json.Unmarshal(data, &eventMsg); err == nil && eventMsg.Event != "" && eventMsg.Event != "update" {
		return nil, nil
	}

	var msg struct {
		Result []json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	if len(msg.Result) == 0 {
		return nil, fmt.Errorf("empty result in position push")
	}

	//nolint:misspell // Gate.io API uses British spelling realized_pnl in JSON response.
	var raw struct {
		Contract           string      `json:"contract"`
		Size               json.Number `json:"size"`
		EntryPrice         json.Number `json:"entry_price"`
		Leverage           json.Number `json:"leverage"`
		CrossLeverageLimit json.Number `json:"cross_leverage_limit"`
		RealisedPnl        json.Number `json:"realised_pnl"`
	}
	if err := json.Unmarshal(msg.Result[0], &raw); err != nil {
		return nil, err
	}

	sizeVal, _ := raw.Size.Float64()
	entryPriceVal, _ := raw.EntryPrice.Float64()
	realisedPnlVal, _ := raw.RealisedPnl.Float64()

	leverageVal, _ := raw.Leverage.Int64()
	leverage := int(leverageVal)
	if leverage == 0 {
		crossLimit, _ := raw.CrossLeverageLimit.Float64()
		leverage = int(crossLimit)
	}

	update := &exchange.PersonalPositionUpdate{
		Symbol:          raw.Contract,
		HoldVol:         math.Abs(sizeVal),
		HoldAvgPrice:    entryPriceVal,
		Leverage:        leverage,
		CloseProfitLoss: realisedPnlVal,
	}

	if sizeVal > 0 {
		update.PositionType = exchange.PositionTypeLong
	} else if sizeVal < 0 {
		update.PositionType = exchange.PositionTypeShort
	}

	return update, nil
}
