package kucoin

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"

	"crypto-bot/pkg/xjson"
)

// Interface assertions.
var (
	_ ws.ExchangeAdapter       = (*WsAdapter)(nil)
	_ exchange.DepthSubscriber = (*WsAdapter)(nil)
	_ exchange.DepthParser     = (*WsAdapter)(nil)
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

// SubscribeDepth subscribes to Level 2 incremental orderbook depth updates.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol string) error {
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

// UnsubscribeDepth unsubscribes from Level 2 incremental orderbook depth updates.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol string) error {
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

func (a *WsAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
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
	if err := xjson.Unmarshal(dataNode, &raw); err != nil {
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
	var positionType exchange.PositionType
	switch {
	case strings.EqualFold(positionSide, "LONG"):
		positionType = exchange.PositionTypeLong // Long
	case strings.EqualFold(positionSide, "SHORT"):
		positionType = exchange.PositionTypeShort // Short
	default:
		if posSize > 0 {
			positionType = exchange.PositionTypeLong // Long
		} else if posSize < 0 {
			positionType = exchange.PositionTypeShort // Short
		}
	}

	update := &exchange.PersonalPositionUpdate{
		Symbol:          symbol,
		HoldVolContract: math.Abs(posSize),
		PositionType:    positionType,
		HoldAvgPrice:    entryPrice,
		OpenAvgPrice:    entryPrice,
		LiquidatePrice:  liqPrice,
		UpdateTime:      ts,
	}

	return update, nil
}

// ParseDepth parses incremental Level 2 depth messages from /contractMarket/level2:{symbol}.
func (a *WsAdapter) ParseDepth(data []byte) (string, *domain.OrderBook, error) {
	topic, _ := jsonparser.GetString(data, "topic")
	symbol := strings.TrimPrefix(topic, "/contractMarket/level2:")
	if symbol == "" {
		symbol, _ = jsonparser.GetString(data, "data", "symbol")
	}

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return symbol, nil, err
	}

	sequence, err := jsonparser.GetInt(dataNode, "sequence")
	if err != nil {
		return symbol, nil, err
	}

	changeStr, err := jsonparser.GetString(dataNode, "change")
	if err != nil {
		return symbol, nil, err
	}

	// Change format: "price,side,size" e.g. "90631.2,sell,2", "3988.50,buy,44", or "3988.61,sell,0"
	parts := strings.Split(changeStr, ",")
	if len(parts) < 3 {
		return symbol, nil, fmt.Errorf("invalid kucoin level2 change format: %s", changeStr)
	}

	price := decmath.ParseFloat(parts[0])
	side := strings.ToLower(strings.TrimSpace(parts[1]))
	size := decmath.ParseFloat(parts[2])

	var bids []domain.OrderBookEntry
	var asks []domain.OrderBookEntry

	entry := domain.OrderBookEntry{Price: price, Volume: size}
	if side == sideBuy || side == "bid" {
		bids = []domain.OrderBookEntry{entry}
	} else {
		asks = []domain.OrderBookEntry{entry}
	}

	ob := &domain.OrderBook{
		Symbol:       symbol,
		FirstVersion: sequence,
		Version:      sequence,
		Bids:         bids,
		Asks:         asks,
	}

	return symbol, ob, nil
}
