package kucoin

import (
	"context"
	"encoding/json"
	"fmt"
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
	pool *pkgws.Pool
}

// NewWsAdapter creates a new KuCoin WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{}
}

// SetPool injects the websocket pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// GetURLFunc returns dynamic URL resolution closure using the bullet token requester.
func GetURLFunc(ctx context.Context, restClient *Client) func() (string, error) {
	return func() (string, error) {
		body, err := restClient.Post(ctx, pathBulletPublic, nil)
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

		data, err := ParseResponse[bulletData](body, "bullet_public")
		if err != nil {
			return "", err
		}

		if len(data.InstanceServers) == 0 {
			return "", fmt.Errorf("no instance servers returned for KuCoin WS")
		}

		srv := data.InstanceServers[0]
		return fmt.Sprintf("%s?token=%s", srv.Endpoint, data.Token), nil
	}
}

// SubscribeTicker subscribes to ticker stream.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	topic := "/contractMarket/tickerV2:" + symbol
	msg := map[string]interface{}{
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
	msg := map[string]interface{}{
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
	msg := map[string]interface{}{
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
	msg := map[string]interface{}{
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
	msg := map[string]interface{}{
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
	msg := map[string]interface{}{
		"id":                "unsub-" + symbol + "-depth",
		paramType:           opUnsubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.pool.UnsubscribePublic(ctx, symbol+":depth", msg)
}

// SubscribePersonal is a placeholder.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	return nil
}

// GetPingConfig returns application ping config.
func (a *WsAdapter) GetPingConfig() (interface{}, time.Duration) {
	return map[string]interface{}{
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
		if subject == "tickerV2" {
			return "tickers"
		}
		if subject == "level2" {
			return "depth"
		}
		if subject == paramKline {
			return paramKline
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

// ParsePosition is a placeholder.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, fmt.Errorf("ParsePosition not implemented on KuCoin WS")
}
