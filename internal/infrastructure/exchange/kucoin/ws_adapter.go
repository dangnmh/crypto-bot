package kucoin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"
)

// WsAdapter implements ws.ExchangeAdapter for KuCoin Futures.
type WsAdapter struct {
	pool       *pkgws.Pool
	restClient *Client
}

// NewWsAdapter creates a new KuCoin WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{}
}

// SetPool injects the websocket pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// SetClient sets the KuCoin REST client on the adapter.
func (a *WsAdapter) SetClient(client *Client) {
	a.restClient = client
}

// GetURLFunc returns dynamic URL resolution closure using the bullet token requester.
func GetURLFunc(ctx context.Context, restClient *Client) func() (string, error) {
	adapter := &WsAdapter{restClient: restClient}
	return adapter.GetPublicURLFunc(ctx)
}

// GetPublicURLFunc returns dynamic URL resolution closure using the bullet token requester.
func (a *WsAdapter) GetPublicURLFunc(ctx context.Context) func() (string, error) {
	return a.getURLFunc(ctx, pathBulletPublic, "bullet_public")
}

// GetPrivateURLFunc returns dynamic URL resolution closure for private channel.
func (a *WsAdapter) GetPrivateURLFunc(ctx context.Context) func() (string, error) {
	return a.getURLFunc(ctx, pathBulletPrivate, "bullet_private")
}

func (a *WsAdapter) getURLFunc(ctx context.Context, path, label string) func() (string, error) {
	return func() (string, error) {
		body, err := a.restClient.Post(ctx, path, nil)
		if err != nil {
			return "", err
		}

		type bulletServer struct {
			Endpoint     string `json:"endpoint"`
			PingInterval int    `json:"pingInterval"`
			PingTimeout  int    `json:"pingTimeout"`
		}

		type bulletData struct {
			Token           string         `json:"token"`
			InstanceServers []bulletServer `json:"instanceServers"`
		}

		data, err := ParseResponse[bulletData](body, label)
		if err != nil {
			return "", err
		}

		if len(data.InstanceServers) == 0 {
			return "", fmt.Errorf("no instance servers returned for KuCoin WS")
		}

		srv := data.InstanceServers[0]
		separator := "?"
		if strings.Contains(srv.Endpoint, "?") {
			separator = "&"
		}
		return fmt.Sprintf("%s%stoken=%s&connectId=%d", srv.Endpoint, separator, data.Token, time.Now().UnixMilli()), nil
	}
}

// SubscribeTicker subscribes to ticker stream.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	topic := "/contractMarket/tickerV2:" + symbol
	msg := map[string]any{
		"id":                "sub-" + symbol + "-ticker",
		paramType:           opSubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.pool.SubscribePublic(ctx, symbol+":tickers", msg)
}

// UnsubscribeTicker unsubscribes from ticker stream.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	topic := "/contractMarket/tickerV2:" + symbol
	msg := map[string]any{
		"id":                "unsub-" + symbol + "-ticker",
		paramType:           opUnsubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.pool.UnsubscribePublic(ctx, symbol+":tickers", msg)
}

// SubscribeKline subscribes to 1-minute klines.
func (a *WsAdapter) SubscribeKline(ctx context.Context, symbol string) error {
	topic := "/contractMarket/kline:" + symbol
	msg := map[string]any{
		"id":                "sub-" + symbol + "-kline",
		paramType:           opSubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.pool.SubscribePublic(ctx, symbol+":kline", msg)
}

// UnsubscribeKline unsubscribes from klines.
func (a *WsAdapter) UnsubscribeKline(ctx context.Context, symbol string) error {
	topic := "/contractMarket/kline:" + symbol
	msg := map[string]any{
		"id":                "unsub-" + symbol + "-kline",
		paramType:           opUnsubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.pool.UnsubscribePublic(ctx, symbol+":kline", msg)
}

// SubscribeDepth subscribes to depth.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol, step string) error {
	topic := "/contractMarket/level2:" + symbol
	msg := map[string]any{
		"id":                "sub-" + symbol + "-depth",
		paramType:           opSubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.pool.SubscribePublic(ctx, symbol+":depth", msg)
}

// UnsubscribeDepth unsubscribes from depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol, step string) error {
	topic := "/contractMarket/level2:" + symbol
	msg := map[string]any{
		"id":                "unsub-" + symbol + "-depth",
		paramType:           opUnsubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.pool.UnsubscribePublic(ctx, symbol+":depth", msg)
}

// SubscribePersonal subscribes to all private futures channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	topic := pathPositionAll
	msg := map[string]any{
		"id":                "sub-personal-position",
		paramType:           opSubscribe,
		paramTopic:          topic,
		paramPrivateChannel: true,
		paramResponse:       true,
	}
	return a.pool.SendPrivate(ctx, msg)
}

// GetPingConfig returns application ping config.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return map[string]any{
		"id":      paramPing,
		paramType: paramPing,
	}, 20 * time.Second
}

// GetAuthHook is a placeholder.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	return nil
}

// GetChannelExtractor maps KuCoin events to channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		subject, _ := jsonparser.GetString(data, "subject")
		topic, _ := jsonparser.GetString(data, "topic")
		if topic == pathPositionAll {
			return constantPersonalPosition
		}

		if subject == "tickerV2" {
			return "ticker"
		}
		if subject == "level2" {
			return "depth"
		}
		if subject == paramKline {
			return paramKline
		}
		if subject == "position.change" {
			return constantPersonalPosition
		}
		return subject
	}
}

// ParseTicker parses ticker feed into store.PriceData.
func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	type wsTicker struct {
		Symbol       string `json:"symbol"`
		Price        string `json:"price"`
		BestBidPrice string `json:"bestBidPrice"`
		BestAskPrice string `json:"bestAskPrice"`
	}

	var raw wsTicker
	if err := json.Unmarshal(dataNode, &raw); err != nil {
		return "", nil, err
	}

	bid := decmath.ParseFloat(raw.BestBidPrice)
	ask := decmath.ParseFloat(raw.BestAskPrice)
	last := decmath.ParseFloat(raw.Price)
	if last == 0 && bid > 0 && ask > 0 {
		last = (bid + ask) / 2.0
	} else if last == 0 {
		last = bid
		if last == 0 {
			last = ask
		}
	}

	pd = &store.PriceData{
		Symbol:    raw.Symbol,
		LastPrice: last,
		BestBid:   bid,
		BestAsk:   ask,
		FairPrice: last,
		Volume24:  0,
		UpdatedAt: time.Now(),
	}

	return raw.Symbol, pd, nil
}

// ParseDepth parses books feed into domain.OrderBook.
func (a *WsAdapter) ParseDepth(data []byte) (symbol string, ob *domain.OrderBook, err error) {
	topic, _ := jsonparser.GetString(data, "topic")
	parts := strings.Split(topic, ":")
	if len(parts) < 2 {
		return "", nil, fmt.Errorf("invalid topic for depth: %s", topic)
	}
	sym := parts[1]

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	type wsDepthLevel struct {
		Price  string `json:"price"`
		Volume string `json:"volume"`
	}

	type wsDepth struct {
		Asks []wsDepthLevel `json:"asks"`
		Bids []wsDepthLevel `json:"bids"`
		Ts   int64          `json:"ts"`
	}

	var raw wsDepth
	if err := json.Unmarshal(dataNode, &raw); err != nil {
		return "", nil, err
	}

	book := &domain.OrderBook{
		Symbol: sym,
		Asks:   make([]domain.OrderBookEntry, 0, len(raw.Asks)),
		Bids:   make([]domain.OrderBookEntry, 0, len(raw.Bids)),
	}

	for _, level := range raw.Asks {
		book.Asks = append(book.Asks, domain.OrderBookEntry{
			Price:  decmath.ParseFloat(level.Price),
			Volume: decmath.ParseFloat(level.Volume),
		})
	}

	for _, level := range raw.Bids {
		book.Bids = append(book.Bids, domain.OrderBookEntry{
			Price:  decmath.ParseFloat(level.Price),
			Volume: decmath.ParseFloat(level.Volume),
		})
	}

	return sym, book, nil
}

// ParseKline is a placeholder.
func (a *WsAdapter) ParseKline(data []byte) (symbol string, k *domain.Kline, err error) {
	return "", nil, fmt.Errorf("ParseKline not implemented on KuCoin WS")
}

// ParseOrder is a placeholder.
func (a *WsAdapter) ParseOrder(data []byte) (*exchange.WsOrderDeal, error) {
	return nil, fmt.Errorf("ParseOrder not implemented on KuCoin WS")
}

// ParseOrderDeal is a placeholder.
func (a *WsAdapter) ParseOrderDeal(data []byte) (*exchange.PersonalOrderDeal, error) {
	return nil, fmt.Errorf("ParseOrderDeal not implemented on KuCoin WS")
}

// ParseTrackOrder is a placeholder.
func (a *WsAdapter) ParseTrackOrder(data []byte) (*exchange.PersonalTrackOrderUpdate, error) {
	return nil, fmt.Errorf("ParseTrackOrder not implemented on KuCoin WS")
}

// ParsePosition parses position updates from the websocket stream.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return nil, err
	}

	symbol, err := jsonparser.GetString(dataNode, "symbol")
	if err != nil {
		return nil, err
	}

	posSize, err := jsonparser.GetFloat(dataNode, "currentQty")
	if err != nil {
		return nil, err
	}

	entryPriceStr, err := jsonparser.GetString(dataNode, "avgEntryPrice")
	var entryPrice float64
	if err == nil {
		entryPrice = decmath.ParseFloat(entryPriceStr)
	} else {
		entryPrice, _ = jsonparser.GetFloat(dataNode, "avgEntryPrice")
	}

	liqPriceStr, err := jsonparser.GetString(dataNode, "liquidationPrice")
	var liqPrice float64
	if err == nil {
		liqPrice = decmath.ParseFloat(liqPriceStr)
	} else {
		liqPrice, _ = jsonparser.GetFloat(dataNode, "liquidationPrice")
	}

	ts, _ := jsonparser.GetInt(dataNode, "currentTimestamp")

	positionSide, _ := jsonparser.GetString(dataNode, "positionSide")
	var positionType int
	switch {
	case strings.EqualFold(positionSide, "LONG"):
		positionType = 1 // Long
	case strings.EqualFold(positionSide, "SHORT"):
		positionType = 2 // Short
	default:
		if posSize > 0 {
			positionType = 1 // Long
		} else if posSize < 0 {
			positionType = 2 // Short
		}
	}

	update := &exchange.PersonalPositionUpdate{
		Symbol:         symbol,
		HoldVol:        math.Abs(posSize),
		PositionType:   positionType,
		HoldAvgPrice:   entryPrice,
		OpenAvgPrice:   entryPrice,
		LiquidatePrice: liqPrice,
		UpdateTime:     ts,
	}

	return update, nil
}
