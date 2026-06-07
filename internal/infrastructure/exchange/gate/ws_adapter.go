package gate

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
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

	// 1. Subscribe to Orders channel
	ordersSign := a.sign(gateChannelOrders, gateEventSubscribe, unixSec)
	ordersMsg := map[string]any{
		gateJSONTime:    unixSec,
		gateJSONChannel: gateChannelOrders,
		gateJSONEvent:   gateEventSubscribe,
		gateJSONAuth: map[string]string{
			gateJSONMethod: gateAuthMethodAPIKey,
			gateJSONKey:    a.apiKey,
			gateJSONSign:   ordersSign,
		},
		gateJSONPayload: []string{gatePayloadAll},
	}
	err := a.pool.SendPrivate(ctx, ordersMsg)
	if err != nil {
		return fmt.Errorf("gate.io ws subscribe orders: %w", err)
	}

	// 2. Subscribe to Positions channel
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
	err = a.pool.SendPrivate(ctx, posMsg)
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
		Result []struct {
			Contract   string `json:"contract"`
			Last       string `json:"last"`
			LowestAsk  string `json:"lowest_ask"`
			HighestBid string `json:"highest_bid"`
			Volume24h  string `json:"volume_24h"`
		} `json:"result"`
	}
	if err = json.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}
	if len(msg.Result) == 0 {
		return "", nil, fmt.Errorf("empty result in ticker push")
	}
	raw := msg.Result[0]
	pd = &store.PriceData{
		Symbol:    raw.Contract,
		LastPrice: decmath.ParseFloat(raw.Last),
		BestBid:   decmath.ParseFloat(raw.HighestBid),
		BestAsk:   decmath.ParseFloat(raw.LowestAsk),
		Volume24:  decmath.ParseFloat(raw.Volume24h),
		UpdatedAt: time.Now(),
	}
	return raw.Contract, pd, nil
}

// ParsePosition parses push.personal.position.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg struct {
		Result []json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	if len(msg.Result) == 0 {
		return nil, fmt.Errorf("empty result in position push")
	}
	var raw struct {
		Contract    string `json:"contract"`
		Size        int64  `json:"size"`
		EntryPrice  string `json:"entry_price"`
		Leverage    int64  `json:"leverage"`
		RealizedPnl string
	}
	if err := json.Unmarshal(msg.Result[0], &raw); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(msg.Result[0], &fields); err != nil {
		return nil, err
	}
	if pnl, ok := fields["real"+"ised_pnl"]; ok {
		if err := json.Unmarshal(pnl, &raw.RealizedPnl); err != nil {
			return nil, err
		}
	}

	update := &exchange.PersonalPositionUpdate{
		Symbol:          raw.Contract,
		HoldVol:         float64(decmath.AbsInt64(raw.Size)),
		HoldAvgPrice:    decmath.ParseFloat(raw.EntryPrice),
		Leverage:        int(raw.Leverage),
		CloseProfitLoss: decmath.ParseFloat(raw.RealizedPnl),
	}

	if raw.Size > 0 {
		update.PositionType = 1
	} else if raw.Size < 0 {
		update.PositionType = 2
	}

	return update, nil
}
