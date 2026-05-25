package gate

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
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
	msg := map[string]interface{}{
		"time":    unixSec,
		"channel": "futures.tickers",
		"event":   "subscribe",
		"payload": []string{symbol},
	}
	topic := symbol + ":ticker"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from ticker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	unixSec := time.Now().Unix()
	msg := map[string]interface{}{
		"time":    unixSec,
		"channel": "futures.tickers",
		"event":   "unsubscribe",
		"payload": []string{symbol},
	}
	topic := symbol + ":ticker"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeKline subscribes to 1-minute klines.
func (a *WsAdapter) SubscribeKline(ctx context.Context, symbol string) error {
	unixSec := time.Now().Unix()
	msg := map[string]interface{}{
		"time":    unixSec,
		"channel": "futures.candlesticks",
		"event":   "subscribe",
		"payload": []string{"1m", symbol},
	}
	topic := symbol + ":kline"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeKline unsubscribes from klines.
func (a *WsAdapter) UnsubscribeKline(ctx context.Context, symbol string) error {
	unixSec := time.Now().Unix()
	msg := map[string]interface{}{
		"time":    unixSec,
		"channel": "futures.candlesticks",
		"event":   "unsubscribe",
		"payload": []string{"1m", symbol},
	}
	topic := symbol + ":kline"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeDepth subscribes to orderbook depth.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol, step string) error {
	unixSec := time.Now().Unix()
	msg := map[string]interface{}{
		"time":    unixSec,
		"channel": "futures.order_book",
		"event":   "subscribe",
		"payload": []string{symbol, "20", "0"}, // symbol, depth, interval in ms ("0" for real-time)
	}
	topic := symbol + ":depth:" + step
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from orderbook depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol, step string) error {
	unixSec := time.Now().Unix()
	msg := map[string]interface{}{
		"time":    unixSec,
		"channel": "futures.order_book",
		"event":   "unsubscribe",
		"payload": []string{symbol, "20", "0"},
	}
	topic := symbol + ":depth:" + step
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to all private futures channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	unixSec := time.Now().Unix()

	// 1. Subscribe to Orders channel
	ordersSign := a.sign("futures.orders", "subscribe", unixSec)
	ordersMsg := map[string]interface{}{
		"time":    unixSec,
		"channel": "futures.orders",
		"event":   "subscribe",
		"auth": map[string]string{
			"method": "api_key",
			"KEY":    a.apiKey,
			"SIGN":   ordersSign,
		},
		"payload": []string{"!all"},
	}
	err := a.pool.SendPrivate(ctx, ordersMsg)
	if err != nil {
		return fmt.Errorf("gate.io ws subscribe orders: %w", err)
	}

	// 2. Subscribe to Positions channel
	posSign := a.sign("futures.positions", "subscribe", unixSec)
	posMsg := map[string]interface{}{
		"time":    unixSec,
		"channel": "futures.positions",
		"event":   "subscribe",
		"auth": map[string]string{
			"method": "api_key",
			"KEY":    a.apiKey,
			"SIGN":   posSign,
		},
		"payload": []string{"!all"},
	}
	err = a.pool.SendPrivate(ctx, posMsg)
	if err != nil {
		return fmt.Errorf("gate.io ws subscribe positions: %w", err)
	}

	return nil
}

// GetPingConfig returns application ping and interval.
func (a *WsAdapter) GetPingConfig() (interface{}, time.Duration) {
	// Gate uses standard ping but some channels support active pings.
	unixSec := time.Now().Unix()
	return map[string]interface{}{
		"time":    unixSec,
		"channel": "futures.ping",
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
			case "futures.tickers":
				return "ticker"
			case "futures.order_book":
				return "depth"
			case "futures.candlesticks":
				return "kline"
			case "futures.orders":
				return "personal.order"
			case "futures.positions":
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

// ParseDepth parses raw JSON into exchange.OrderBook.
func (a *WsAdapter) ParseDepth(data []byte) (symbol string, ob *domain.OrderBook, err error) {
	var msg struct {
		Result struct {
			Contract string `json:"contract"`
			Asks     []struct {
				P string `json:"p"`
				S string `json:"s"`
			} `json:"asks"`
			Bids []struct {
				P string `json:"p"`
				S string `json:"s"`
			} `json:"bids"`
		} `json:"result"`
	}
	if err = json.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}
	raw := msg.Result
	ob = &domain.OrderBook{
		Symbol: raw.Contract,
		Asks:   make([]exchange.OrderBookEntry, 0, len(raw.Asks)),
		Bids:   make([]exchange.OrderBookEntry, 0, len(raw.Bids)),
	}
	for _, item := range raw.Asks {
		p := decmath.ParseFloat(item.P)
		v := decmath.ParseFloat(item.S)
		if p > 0 {
			ob.Asks = append(ob.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}
	for _, item := range raw.Bids {
		p := decmath.ParseFloat(item.P)
		v := decmath.ParseFloat(item.S)
		if p > 0 {
			ob.Bids = append(ob.Bids, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}
	return raw.Contract, ob, nil
}

// ParseKline parses raw JSON into exchange.Kline.
func (a *WsAdapter) ParseKline(data []byte) (symbol string, k *exchange.Kline, err error) {
	var msg struct {
		Result []struct {
			T int64  `json:"t"`
			V string `json:"v"`
			C string `json:"c"`
			H string `json:"h"`
			L string `json:"l"`
			O string `json:"o"`
			N string `json:"n"`
		} `json:"result"`
	}
	if err = json.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}
	if len(msg.Result) == 0 {
		return "", nil, fmt.Errorf("empty result in kline push")
	}
	raw := msg.Result[0]
	parts := strings.SplitN(raw.N, "_", 2)
	symbol = raw.N
	if len(parts) == 2 {
		symbol = parts[1]
	}
	k = &exchange.Kline{
		Timestamp: raw.T * 1000,
		Open:      decmath.ParseFloat(raw.O),
		Close:     decmath.ParseFloat(raw.C),
		High:      decmath.ParseFloat(raw.H),
		Low:       decmath.ParseFloat(raw.L),
		Volume:    decmath.ParseFloat(raw.V),
	}
	return symbol, k, nil
}

// ParseOrder parses raw JSON into exchange.WsOrderDeal.
func (a *WsAdapter) ParseOrder(data []byte) (*exchange.WsOrderDeal, error) {
	var msg struct {
		Result []struct {
			Id       int64  `json:"id"`
			Contract string `json:"contract"`
			Size     int64  `json:"size"`
			Price    string `json:"price"`
			Status   string `json:"status"`
			FinishAs string `json:"finish_as"`
			Left     int64  `json:"left"`
			Text     string `json:"text"`
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	if len(msg.Result) == 0 {
		return nil, fmt.Errorf("empty result in order push")
	}
	raw := msg.Result[0]

	deal := &exchange.WsOrderDeal{
		Symbol:  raw.Contract,
		OrderID: strconv.FormatInt(raw.Id, 10),
		Price:   decmath.ParseFloat(raw.Price),
		Vol:     float64(decmath.AbsInt64(raw.Size)),
		DealVol: float64(decmath.AbsInt64(raw.Size) - decmath.AbsInt64(raw.Left)),
	}

	if raw.Status == "finished" {
		if raw.FinishAs == "filled" {
			deal.State = exchange.OrderStateFilled
		} else {
			deal.State = exchange.OrderStateCanceled
		}
	} else if raw.Status == "open" {
		deal.State = exchange.OrderStatePartial
	}

	if strings.HasPrefix(raw.Text, "t-") {
		deal.ExternalOID = strings.TrimPrefix(raw.Text, "t-")
	} else {
		deal.ExternalOID = raw.Text
	}

	return deal, nil
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
		Result []struct {
			Contract    string `json:"contract"`
			Size        int64  `json:"size"`
			EntryPrice  string `json:"entry_price"`
			Leverage    int64  `json:"leverage"`
			RealisedPnl string `json:"realised_pnl"` //nolint:misspell // API returns 'realised_pnl'
		} `json:"result"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}
	if len(msg.Result) == 0 {
		return nil, fmt.Errorf("empty result in position push")
	}
	raw := msg.Result[0]

	update := &exchange.PersonalPositionUpdate{
		Symbol:       raw.Contract,
		HoldVol:      float64(decmath.AbsInt64(raw.Size)),
		HoldAvgPrice: decmath.ParseFloat(raw.EntryPrice),
		Leverage:     int(raw.Leverage),
		Realized:     decmath.ParseFloat(raw.RealisedPnl),
	}

	if raw.Size > 0 {
		update.PositionType = 1
	} else if raw.Size < 0 {
		update.PositionType = 2
	}

	return update, nil
}
