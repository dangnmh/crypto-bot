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
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"

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
		if strings.HasPrefix(topic, "/market/level2:") {
			return "depth"
		}
		return ""
	}
}

// ParseDepth parses spot Level 2 depth updates.
func (a *WsAdapter) ParseDepth(data []byte) (string, *domain.OrderBook, error) {
	topic, _ := jsonparser.GetString(data, "topic")
	symbol := strings.TrimPrefix(topic, "/market/level2:")
	if symbol == "" {
		symbol, _ = jsonparser.GetString(data, "data", "symbol")
	}

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return symbol, nil, err
	}

	seqStart, _ := jsonparser.GetInt(dataNode, "sequenceStart")
	seqEnd, _ := jsonparser.GetInt(dataNode, "sequenceEnd")
	if seqEnd == 0 {
		seqEnd = seqStart
	}
	if seqStart == 0 {
		seqStart = seqEnd
	}

	var bids []domain.OrderBookEntry
	_, _ = jsonparser.ArrayEach(dataNode, func(val []byte, dataType jsonparser.ValueType, offset int, err error) {
		pStr, _ := jsonparser.GetString(val, "[0]")
		vStr, _ := jsonparser.GetString(val, "[1]")
		p, v := decmath.ParseFloat(pStr), decmath.ParseFloat(vStr)
		if p > 0 {
			bids = append(bids, domain.OrderBookEntry{Price: p, Volume: v})
		}
	}, "changes", "bids")

	var asks []domain.OrderBookEntry
	_, _ = jsonparser.ArrayEach(dataNode, func(val []byte, dataType jsonparser.ValueType, offset int, err error) {
		pStr, _ := jsonparser.GetString(val, "[0]")
		vStr, _ := jsonparser.GetString(val, "[1]")
		p, v := decmath.ParseFloat(pStr), decmath.ParseFloat(vStr)
		if p > 0 {
			asks = append(asks, domain.OrderBookEntry{Price: p, Volume: v})
		}
	}, "changes", "asks")

	return symbol, &domain.OrderBook{
		Symbol:       symbol,
		FirstVersion: seqStart,
		Version:      seqEnd,
		Bids:         bids,
		Asks:         asks,
	}, nil
}

// ParseTicker is a no-op for spot in this phase.
func (a *WsAdapter) ParseTicker(data []byte) (string, *store.PriceData, error) {
	return "", nil, nil
}

// ParsePosition is a no-op for spot.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, nil
}
