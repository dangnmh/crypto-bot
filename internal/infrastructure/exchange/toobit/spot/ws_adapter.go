package spot

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
	"crypto-bot/pkg/xjson"
)

const (
	binaryKey = "binary"
	depthKey  = "depth"
	eventKey  = "event"
	paramsKey = "params"
	symbolKey = "symbol"
	topicKey  = "topic"
)

var (
	_ exchange.DepthSubscriber = (*WsAdapter)(nil)
	_ exchange.DepthParser     = (*WsAdapter)(nil)
)

// WsAdapter implements ws.ExchangeAdapter for Toobit Spot.
type WsAdapter struct {
	pool       *pkgws.Pool
	restClient *Client
}

// NewWsAdapter creates a new Toobit Spot WsAdapter.
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

// SetClient sets the client reference.
func (a *WsAdapter) SetClient(client *Client) {
	a.restClient = client
}

// SubscribeTicker is a no-op for spot in this phase.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	return nil
}

func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	return nil
}

// SubscribeDepth streams spot orderbook depth.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol string) error {
	msg := map[string]any{
		eventKey:  "sub",
		topicKey:  depthKey,
		symbolKey: symbol,
		paramsKey: map[string]any{
			binaryKey: false,
		},
	}
	return a.SubscribePublic(ctx, symbol+":depth", msg)
}

// UnsubscribeDepth stops streaming spot orderbook depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol string) error {
	msg := map[string]any{
		eventKey:  "cancel",
		topicKey:  depthKey,
		symbolKey: symbol,
		paramsKey: map[string]any{
			binaryKey: false,
		},
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

// GetPingConfig returns application ping parameters.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return map[string]any{"ping": 1700000000000}, 30 * time.Second
}

// GetAuthHook returns nil for public spot.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	return nil
}

// GetChannelExtractor maps WebSocket event keys to handler channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		var msg struct {
			Topic string `json:"topic"`
			Event string `json:"event"`
		}
		if err := xjson.Unmarshal(data, &msg); err == nil {
			if msg.Topic == depthKey || msg.Event == depthKey {
				return depthKey
			}
		}
		return ""
	}
}

type wsDepthEntry struct {
	Bids [][]xjson.Number `json:"b"`
	Asks [][]xjson.Number `json:"a"`
	V    json.RawMessage  `json:"v"`
	T    int64            `json:"t"`
}

func parseToobitDepthVersion(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	s := strings.Trim(string(raw), "\"")
	if idx := strings.Index(s, "_"); idx != -1 {
		s = s[:idx]
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// ParseDepth parses spot depth messages.
func (a *WsAdapter) ParseDepth(data []byte) (string, *domain.OrderBook, error) {
	var basic struct {
		Topic  string          `json:"topic"`
		Symbol string          `json:"symbol"`
		Data   json.RawMessage `json:"data"`
	}
	if err := xjson.Unmarshal(data, &basic); err != nil {
		return "", nil, fmt.Errorf("unmarshal spot depth basic: %w", err)
	}

	sym := basic.Symbol

	var entries []wsDepthEntry
	if err := xjson.Unmarshal(basic.Data, &entries); err != nil {
		var single wsDepthEntry
		if err2 := xjson.Unmarshal(basic.Data, &single); err2 != nil {
			return "", nil, fmt.Errorf("unmarshal spot depth data: %w", err)
		}
		entries = []wsDepthEntry{single}
	}

	if len(entries) == 0 {
		return sym, nil, nil
	}

	latest := entries[len(entries)-1]
	bids := make([]domain.OrderBookEntry, 0, len(latest.Bids))
	for _, b := range latest.Bids {
		if len(b) >= 2 {
			p := xjson.ToFloat64(b[0])
			v := xjson.ToFloat64(b[1])
			if p > 0 && v > 0 {
				bids = append(bids, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	asks := make([]domain.OrderBookEntry, 0, len(latest.Asks))
	for _, a := range latest.Asks {
		if len(a) >= 2 {
			p := xjson.ToFloat64(a[0])
			v := xjson.ToFloat64(a[1])
			if p > 0 && v > 0 {
				asks = append(asks, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	return sym, &domain.OrderBook{
		Symbol:  sym,
		Version: parseToobitDepthVersion(latest.V),
		Bids:    bids,
		Asks:    asks,
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
