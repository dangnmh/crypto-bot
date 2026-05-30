package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/samber/lo"
	hl "github.com/sonirico/go-hyperliquid"
)

// WsAdapter implements ws.ExchangeAdapter for Hyperliquid WebSocket.
type WsAdapter struct {
	pool        *pkgws.Pool
	userAddress string
}

// NewWsAdapter creates a new WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{}
}

// SetPool injects the websocket connection pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// SubscribeTicker subscribes to mid prices using allMids channel.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: opSubscribe,
		fieldSubscription: map[string]any{
			fieldType: channelAllMids,
		},
	}
	topic := symbol + ":tickers"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from mid prices.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: opUnsubscribe,
		fieldSubscription: map[string]any{
			fieldType: channelAllMids,
		},
	}
	topic := symbol + ":tickers"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeKline subscribes to candlesticks.
func (a *WsAdapter) SubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: opSubscribe,
		fieldSubscription: map[string]any{
			fieldType:     channelCandle,
			fieldCoin:     symbol,
			fieldInterval: "1m",
		},
	}
	topic := symbol + ":kline"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeKline unsubscribes from candlesticks.
func (a *WsAdapter) UnsubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: opUnsubscribe,
		fieldSubscription: map[string]any{
			fieldType:     channelCandle,
			fieldCoin:     symbol,
			fieldInterval: "1m",
		},
	}
	topic := symbol + ":kline"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeDepth subscribes to L2 orderbook book.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol, step string) error {
	msg := map[string]any{
		paramMethod: opSubscribe,
		fieldSubscription: map[string]any{
			fieldType: channelL2Book,
			fieldCoin: symbol,
		},
	}
	topic := symbol + ":depth"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from L2 orderbook.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol, step string) error {
	msg := map[string]any{
		paramMethod: opUnsubscribe,
		fieldSubscription: map[string]any{
			fieldType: channelL2Book,
			fieldCoin: symbol,
		},
	}
	topic := symbol + ":depth"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to user events (fills, orders).
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	if a.userAddress == "" {
		return fmt.Errorf("userAddress is not initialized for WS auth")
	}

	msg := map[string]any{
		paramMethod: opSubscribe,
		fieldSubscription: map[string]any{
			fieldType: "userEvents",
			"user":    a.userAddress,
		},
	}
	return a.pool.SendPrivate(ctx, msg)
}

// GetPingConfig returns application ping and interval.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return map[string]any{
		"method": "ping",
	}, 30 * time.Second
}

// GetAuthHook intercepts OnConnected to derive userAddress.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	if apiSecret != "" {
		pk, err := crypto.HexToECDSA(strings.TrimPrefix(apiSecret, "0x"))
		if err == nil {
			a.userAddress = crypto.PubkeyToAddress(pk.PublicKey).Hex()
		}
	}
	return nil
}

// GetChannelExtractor routes incoming WS channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		if string(data) == `"pong"` || string(data) == "pong" {
			return msgPong
		}

		channel, err := jsonparser.GetString(data, "channel")
		if err != nil {
			return ""
		}

		switch channel {
		case channelAllMids:
			return "tickers"
		case channelL2Book:
			return "depth"
		case channelCandle:
			return "kline"
		case channelUser:
			return "personal.order"
		}
		return channel
	}
}

// ParseTicker parses allMids push.
func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	midsBytes, _, _, err := jsonparser.Get(data, "data", "mids")
	if err != nil {
		return "", nil, err
	}

	var parsedSymbol string
	var price float64
	_ = jsonparser.ObjectEach(midsBytes, func(key []byte, value []byte, dataType jsonparser.ValueType, offset int) error {
		parsedSymbol = string(key)
		price, _ = strconv.ParseFloat(string(value), 64)
		return fmt.Errorf("stop")
	})

	if parsedSymbol == "" {
		return "", nil, fmt.Errorf("no mids found")
	}

	pd = &store.PriceData{
		Symbol:    parsedSymbol,
		LastPrice: price,
		BestBid:   price,
		BestAsk:   price,
		FairPrice: price,
		Volume24:  0,
		UpdatedAt: time.Now(),
	}
	return parsedSymbol, pd, nil
}

// ParseDepth parses l2Book levels.
func (a *WsAdapter) ParseDepth(data []byte) (symbol string, ob *domain.OrderBook, err error) {
	symbol, err = jsonparser.GetString(data, "data", "coin")
	if err != nil {
		return "", nil, err
	}

	bidsBytes, _, _, err := jsonparser.Get(data, "data", "levels", "[0]")
	if err != nil {
		return "", nil, err
	}

	asksBytes, _, _, err := jsonparser.Get(data, "data", "levels", "[1]")
	if err != nil {
		return "", nil, err
	}

	var hlBids []hl.Level
	if err := json.Unmarshal(bidsBytes, &hlBids); err != nil {
		return "", nil, err
	}

	var hlAsks []hl.Level
	if err := json.Unmarshal(asksBytes, &hlAsks); err != nil {
		return "", nil, err
	}

	bids := make([]exchange.OrderBookEntry, 0, len(hlBids))
	for i := range hlBids {
		bids = append(bids, exchange.OrderBookEntry{
			Price:  hlBids[i].Px,
			Volume: hlBids[i].Sz,
		})
	}

	asks := make([]exchange.OrderBookEntry, 0, len(hlAsks))
	for i := range hlAsks {
		asks = append(asks, exchange.OrderBookEntry{
			Price:  hlAsks[i].Px,
			Volume: hlAsks[i].Sz,
		})
	}

	ob = &domain.OrderBook{
		Symbol: symbol,
		Bids:   bids,
		Asks:   asks,
	}

	return symbol, ob, nil
}

// ParseDepthCommit is a stub.
func (a *WsAdapter) ParseDepthCommit(data []byte) (symbol string, commit *exchange.DepthCommit, err error) {
	return "", nil, nil
}

// ParseKline parses candle push.
func (a *WsAdapter) ParseKline(data []byte) (symbol string, k *exchange.Kline, err error) {
	candleBytes, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	var cand hl.Candle
	if err := json.Unmarshal(candleBytes, &cand); err != nil {
		return "", nil, err
	}

	open, _ := strconv.ParseFloat(cand.Open, 64)
	high, _ := strconv.ParseFloat(cand.High, 64)
	low, _ := strconv.ParseFloat(cand.Low, 64)
	closeVal, _ := strconv.ParseFloat(cand.Close, 64)
	vol, _ := strconv.ParseFloat(cand.Volume, 64)

	k = &exchange.Kline{
		Timestamp: cand.TimeOpen,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     closeVal,
		Volume:    vol,
		Amount:    vol * closeVal,
	}
	return cand.Symbol, k, nil
}

// ParseOrder parses private userEvents order updates.
func (a *WsAdapter) ParseOrder(data []byte) (*exchange.WsOrderDeal, error) {
	ordersBytes, _, _, err := jsonparser.Get(data, "data", "orders")
	if err != nil {
		return nil, err
	}

	var orders []hl.WsOrder
	if err := json.Unmarshal(ordersBytes, &orders); err != nil {
		return nil, err
	}

	if len(orders) == 0 {
		return nil, fmt.Errorf("empty orders array")
	}

	o := &orders[0]
	raw := o.Order

	price, _ := strconv.ParseFloat(raw.LimitPx, 64)
	origSz, _ := strconv.ParseFloat(raw.OrigSz, 64)
	sz, _ := strconv.ParseFloat(raw.Sz, 64)

	isBuy := raw.Side == "B"
	side := exchange.SideOpenLong
	if !isBuy {
		side = exchange.SideOpenShort
	}

	state := exchange.OrderStatePartial
	switch o.Status {
	case stateFilled:
		state = exchange.OrderStateFilled
	case stateCanceled:
		state = exchange.OrderStateCanceled
	default:
	}

	deal := &exchange.WsOrderDeal{
		Symbol:       raw.Coin,
		OrderID:      strconv.FormatInt(raw.Oid, 10),
		Price:        price,
		Vol:          origSz,
		DealVol:      origSz - sz,
		DealAvgPrice: price,
		Side:         side,
		State:        state,
		PositionMode: 1,
	}

	if raw.Cloid != nil {
		deal.ExternalOID = lo.FromPtr(raw.Cloid)
	}

	return deal, nil
}

// ParseOrderDeal is a stub.
func (a *WsAdapter) ParseOrderDeal(data []byte) (*exchange.PersonalOrderDeal, error) {
	return nil, nil
}

// ParseTrackOrder is a stub.
func (a *WsAdapter) ParseTrackOrder(data []byte) (*exchange.PersonalTrackOrderUpdate, error) {
	return nil, nil
}

// ParsePosition is a stub.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, nil
}
