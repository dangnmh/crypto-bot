package futures

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"

	"github.com/buger/jsonparser"
)

var (
	_ exchange.DepthSubscriber = (*WsAdapter)(nil)
	_ exchange.DepthParser     = (*WsAdapter)(nil)
	_ exchange.TradeSubscriber = (*WsAdapter)(nil)
	_ exchange.TradeParser     = (*WsAdapter)(nil)
)

// WsAdapter implements ws.ExchangeAdapter for KuCoin Futures.
type WsAdapter struct {
	pool       *pkgws.Pool
	restClient *Client
}

// NewWsAdapter creates a new KuCoin Futures WsAdapter.
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

// GetPublicURLFunc returns dynamic URL resolution closure using the bullet token requester.
func (a *WsAdapter) GetPublicURLFunc(ctx context.Context) func() (string, error) {
	return a.getURLFunc(ctx, "/api/v1/bullet-public", "bullet_public")
}

// GetPrivateURLFunc returns dynamic URL resolution closure for private channel.
func (a *WsAdapter) GetPrivateURLFunc(ctx context.Context) func() (string, error) {
	return a.getURLFunc(ctx, "/api/v1/bullet-private", "bullet_private")
}

func (a *WsAdapter) getURLFunc(ctx context.Context, path, label string) func() (string, error) {
	return func() (string, error) {
		if a.restClient == nil {
			return "", fmt.Errorf("rest client is nil")
		}
		signed := path == "/api/v1/bullet-private"
		body, err := a.restClient.BaseClient().Request(ctx, http.MethodPost, path, nil, nil, signed)
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

		data, err := kucoin.ParseResponse[bulletData](body, label)
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
	return a.SubscribePublic(ctx, symbol+":tickers", msg)
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
	return a.UnsubscribePublic(ctx, symbol+":tickers", msg)
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
	return a.SubscribePublic(ctx, symbol+":depth", msg)
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
	return a.UnsubscribePublic(ctx, symbol+":depth", msg)
}

// SubscribeTrade subscribes to real-time order match execution trade stream.
func (a *WsAdapter) SubscribeTrade(ctx context.Context, symbol string) error {
	topic := "/contractMarket/execution:" + symbol
	msg := map[string]any{
		"id":                "sub-" + symbol + "-trade",
		paramType:           opSubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.SubscribePublic(ctx, symbol+":trade", msg)
}

// UnsubscribeTrade unsubscribes from real-time order match execution trade stream.
func (a *WsAdapter) UnsubscribeTrade(ctx context.Context, symbol string) error {
	topic := "/contractMarket/execution:" + symbol
	msg := map[string]any{
		"id":                "unsub-" + symbol + "-trade",
		paramType:           opUnsubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.UnsubscribePublic(ctx, symbol+":trade", msg)
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
	if a.pool == nil {
		return nil
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

// GetAuthHook is nil for KuCoin.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	return nil
}

// GetChannelExtractor maps KuCoin events to channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		subject, _ := jsonparser.GetString(data, "subject")
		topic, _ := jsonparser.GetString(data, "topic")
		if topic == pathPositionAll {
			return paramPersonalPosition
		}

		if strings.HasPrefix(topic, "/contractMarket/level2:") || subject == "level2" || subject == "trade.l2update" {
			return "depth"
		}
		if strings.HasPrefix(topic, "/contractMarket/execution:") || subject == "match" {
			return "trade"
		}
		if strings.HasPrefix(topic, "/contractMarket/tickerV2:") || subject == "tickerV2" || subject == "trade.ticker" {
			return "ticker"
		}
		if subject == "position.change" {
			return "personal.position"
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
		Symbol       string       `json:"symbol"`
		Price        xjson.Number `json:"price"`
		BestBidPrice xjson.Number `json:"bestBidPrice"`
		BestAskPrice xjson.Number `json:"bestAskPrice"`
	}

	var raw wsTicker
	if err := xjson.Unmarshal(dataNode, &raw); err != nil {
		return "", nil, err
	}

	bid := xjson.ToFloat64(raw.BestBidPrice)
	ask := xjson.ToFloat64(raw.BestAskPrice)
	last := xjson.ToFloat64(raw.Price)
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
		positionType = exchange.PositionTypeLong
	case strings.EqualFold(positionSide, "SHORT"):
		positionType = exchange.PositionTypeShort
	default:
		if posSize > 0 {
			positionType = exchange.PositionTypeLong
		} else if posSize < 0 {
			positionType = exchange.PositionTypeShort
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

type wsDepthMessage struct {
	Topic string `json:"topic"`
	Data  struct {
		Sequence  int64  `json:"sequence"`
		Change    string `json:"change"`
		Timestamp int64  `json:"timestamp"`
		Symbol    string `json:"symbol"`
	} `json:"data"`
}

// ParseDepth parses incremental Level 2 depth messages from /contractMarket/level2:{symbol}.
func (a *WsAdapter) ParseDepth(data []byte) (string, *domain.OrderBook, error) {
	var msg wsDepthMessage
	if err := xjson.Unmarshal(data, &msg); err != nil {
		return "", nil, fmt.Errorf("unmarshal kucoin futures depth: %w", err)
	}

	symbol := strings.TrimPrefix(msg.Topic, "/contractMarket/level2:")
	if symbol == "" {
		symbol = msg.Data.Symbol
	}

	parts := strings.Split(msg.Data.Change, ",")
	if len(parts) < 3 {
		return symbol, nil, fmt.Errorf("invalid kucoin level2 change format: %s", msg.Data.Change)
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

	ts := time.Now().UTC()
	if msg.Data.Timestamp > 0 {
		ts = time.UnixMilli(msg.Data.Timestamp).UTC()
	}

	ob := &domain.OrderBook{
		Symbol:       symbol,
		FirstVersion: msg.Data.Sequence,
		Version:      msg.Data.Sequence,
		Timestamp:    ts,
		Bids:         bids,
		Asks:         asks,
	}

	return symbol, ob, nil
}

type wsTradeMessage struct {
	Topic   string `json:"topic"`
	Type    string `json:"type"`
	Subject string `json:"subject"`
	Data    struct {
		Symbol       string       `json:"symbol"`
		Sequence     int64        `json:"sequence"`
		Side         string       `json:"side"`
		Size         xjson.Number `json:"size"`
		Price        xjson.Number `json:"price"`
		TakerOrderID string       `json:"takerOrderId"`
		MakerOrderID string       `json:"makerOrderId"`
		TradeID      string       `json:"tradeId"`
		TS           int64        `json:"ts"`
	} `json:"data"`
}

// ParseTrade parses real-time match execution trade messages into []domain.PublicTrade.
func (a *WsAdapter) ParseTrade(data []byte) (string, []domain.PublicTrade, error) {
	var msg wsTradeMessage
	if err := xjson.Unmarshal(data, &msg); err != nil {
		return "", nil, fmt.Errorf("unmarshal kucoin futures trade: %w", err)
	}

	symbol := msg.Data.Symbol
	if symbol == "" {
		if idx := strings.LastIndex(msg.Topic, ":"); idx != -1 && idx < len(msg.Topic)-1 {
			symbol = msg.Topic[idx+1:]
		}
	}

	p, _ := msg.Data.Price.Float64()
	v, _ := msg.Data.Size.Float64()
	if p <= 0 || v <= 0 {
		return symbol, nil, nil
	}

	side := domain.SideOpenLong
	if strings.EqualFold(msg.Data.Side, "sell") {
		side = domain.SideOpenShort
	}

	ts := time.Now().UTC()
	if msg.Data.TS > 0 {
		if msg.Data.TS > 1e16 {
			ts = time.Unix(0, msg.Data.TS).UTC()
		} else {
			ts = time.UnixMilli(msg.Data.TS).UTC()
		}
	}

	trade := domain.PublicTrade{
		Symbol:    symbol,
		Price:     p,
		Volume:    v,
		Side:      side,
		Timestamp: ts,
	}

	return symbol, []domain.PublicTrade{trade}, nil
}
