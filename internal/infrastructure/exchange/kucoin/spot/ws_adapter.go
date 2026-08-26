package spot

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"

	"github.com/buger/jsonparser"
)

const (
	paramPrivateChannel = "privateChannel"
	paramPing           = "ping"
	paramType           = "type"
	paramTopic          = "topic"
	paramResponse       = "response"
	opSubscribe         = "subscribe"
	opUnsubscribe       = "unsubscribe"
)

var (
	_ exchange.DepthSubscriber = (*WsAdapter)(nil)
	_ exchange.DepthParser     = (*WsAdapter)(nil)
	_ exchange.TradeSubscriber = (*WsAdapter)(nil)
	_ exchange.TradeParser     = (*WsAdapter)(nil)
)

// WsAdapter implements ws.ExchangeAdapter for KuCoin Spot.
type WsAdapter struct {
	pool       *pkgws.Pool
	restClient *Client
}

// NewWsAdapter creates a new KuCoin Spot WsAdapter.
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

// GetPublicURLFunc returns dynamic URL resolution closure using bullet token.
func (a *WsAdapter) GetPublicURLFunc(ctx context.Context) func() (string, error) {
	return func() (string, error) {
		if a.restClient == nil {
			return "", fmt.Errorf("rest client is nil")
		}
		body, err := a.restClient.BaseClient().Request(ctx, http.MethodPost, "/api/v1/bullet-public", nil, nil, false)
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

		data, err := kucoin.ParseResponse[bulletData](body, "bullet_public")
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

// SubscribeTicker is a no-op for spot in this phase.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	return nil
}

func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	return nil
}

// SubscribeDepth subscribes to Level 2 spot orderbook depth updates.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol string) error {
	topic := "/market/level2:" + symbol
	msg := map[string]any{
		"id":                "sub-" + symbol + "-depth",
		paramType:           opSubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.SubscribePublic(ctx, symbol+":depth", msg)
}

// UnsubscribeDepth unsubscribes from Level 2 spot orderbook depth updates.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol string) error {
	topic := "/market/level2:" + symbol
	msg := map[string]any{
		"id":                "unsub-" + symbol + "-depth",
		paramType:           opUnsubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.UnsubscribePublic(ctx, symbol+":depth", msg)
}

// SubscribeTrade subscribes to Level 3 trade match execution stream for a spot symbol.
func (a *WsAdapter) SubscribeTrade(ctx context.Context, symbol string) error {
	topic := "/market/match:" + symbol
	msg := map[string]any{
		"id":                "sub-" + symbol + "-trade",
		paramType:           opSubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.SubscribePublic(ctx, symbol+":trade", msg)
}

// UnsubscribeTrade unsubscribes from Level 3 trade match execution stream for a spot symbol.
func (a *WsAdapter) UnsubscribeTrade(ctx context.Context, symbol string) error {
	topic := "/market/match:" + symbol
	msg := map[string]any{
		"id":                "unsub-" + symbol + "-trade",
		paramType:           opUnsubscribe,
		paramTopic:          topic,
		paramPrivateChannel: false,
		paramResponse:       true,
	}
	return a.UnsubscribePublic(ctx, symbol+":trade", msg)
}

// SubscribePersonal is a no-op for spot in this phase.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	return nil
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

// GetAuthHook returns nil for public spot streams.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	return nil
}

// GetChannelExtractor maps KuCoin Spot events to channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		topic, _ := jsonparser.GetString(data, "topic")
		subject, _ := jsonparser.GetString(data, "subject")
		if strings.HasPrefix(topic, "/market/level2:") || subject == "level2" || subject == "trade.l2update" {
			return "depth"
		}
		if strings.HasPrefix(topic, "/market/match:") || subject == "trade.l3match" || subject == "match" {
			return "trade"
		}
		return ""
	}
}

type wsDepthMessage struct {
	Topic string `json:"topic"`
	Data  struct {
		SequenceStart int64  `json:"sequenceStart"`
		SequenceEnd   int64  `json:"sequenceEnd"`
		Symbol        string `json:"symbol"`
		Time          int64  `json:"time"`
		Changes       struct {
			Bids [][]xjson.Number `json:"bids"`
			Asks [][]xjson.Number `json:"asks"`
		} `json:"changes"`
	} `json:"data"`
}

// ParseDepth parses spot Level 2 depth updates.
func (a *WsAdapter) ParseDepth(data []byte) (string, *domain.OrderBook, error) {
	var msg wsDepthMessage
	if err := xjson.Unmarshal(data, &msg); err != nil {
		return "", nil, fmt.Errorf("unmarshal kucoin spot depth: %w", err)
	}

	symbol := strings.TrimPrefix(msg.Topic, "/market/level2:")
	if symbol == "" {
		symbol = msg.Data.Symbol
	}

	bids := make([]domain.OrderBookEntry, 0, len(msg.Data.Changes.Bids))
	for _, item := range msg.Data.Changes.Bids {
		if len(item) >= 2 {
			p := xjson.ToFloat64(item[0])
			v := xjson.ToFloat64(item[1])
			if p > 0 {
				bids = append(bids, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	asks := make([]domain.OrderBookEntry, 0, len(msg.Data.Changes.Asks))
	for _, item := range msg.Data.Changes.Asks {
		if len(item) >= 2 {
			p := xjson.ToFloat64(item[0])
			v := xjson.ToFloat64(item[1])
			if p > 0 {
				asks = append(asks, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	seqStart := msg.Data.SequenceStart
	seqEnd := msg.Data.SequenceEnd
	if seqEnd == 0 {
		seqEnd = seqStart
	}
	if seqStart == 0 {
		seqStart = seqEnd
	}

	timeVal := msg.Data.Time
	ts := time.Now().UTC()
	if timeVal > 0 {
		ts = time.UnixMilli(timeVal).UTC()
	}

	return symbol, &domain.OrderBook{
		Symbol:       symbol,
		FirstVersion: seqStart,
		Version:      seqEnd,
		Timestamp:    ts,
		Bids:         bids,
		Asks:         asks,
	}, nil
}

type wsTradeMessage struct {
	Topic   string `json:"topic"`
	Type    string `json:"type"`
	Subject string `json:"subject"`
	Data    struct {
		Symbol       string       `json:"symbol"`
		Sequence     string       `json:"sequence"`
		Side         string       `json:"side"`
		Size         xjson.Number `json:"size"`
		Price        xjson.Number `json:"price"`
		MakerOrderID string       `json:"makerOrderId"`
		TakerOrderID string       `json:"takerOrderId"`
		TradeID      string       `json:"tradeId"`
		Type         string       `json:"type"`
		Time         xjson.Number `json:"time"`
	} `json:"data"`
}

// ParseTrade parses Level 3 match execution trade messages into []domain.PublicTrade.
func (a *WsAdapter) ParseTrade(data []byte) (string, []domain.PublicTrade, error) {
	var msg wsTradeMessage
	if err := xjson.Unmarshal(data, &msg); err != nil {
		return "", nil, fmt.Errorf("unmarshal kucoin spot trade: %w", err)
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
	timeNum, _ := msg.Data.Time.Int64()
	if timeNum > 0 {
		if timeNum > 1e16 {
			ts = time.Unix(0, timeNum).UTC()
		} else {
			ts = time.UnixMilli(timeNum).UTC()
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

// ParseTicker is a no-op for spot in this phase.
func (a *WsAdapter) ParseTicker(data []byte) (string, *store.PriceData, error) {
	return "", nil, nil
}

// ParsePosition is a no-op for spot.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, nil
}
