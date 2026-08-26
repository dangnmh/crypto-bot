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
	binaryKey   = "binary"
	depthKey    = "depth"
	tradeKey    = "trade"
	eventKey    = "event"
	eventSub    = "sub"
	eventCancel = "cancel"
	paramsKey   = "params"
	symbolKey   = "symbol"
	topicKey    = "topic"
)

var (
	_ exchange.DepthSubscriber = (*WsAdapter)(nil)
	_ exchange.DepthParser     = (*WsAdapter)(nil)
	_ exchange.TradeSubscriber = (*WsAdapter)(nil)
	_ exchange.TradeParser     = (*WsAdapter)(nil)
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
	sym := formatSpotSymbol(symbol)
	msg := map[string]any{
		eventKey:  eventSub,
		topicKey:  depthKey,
		symbolKey: sym,
		paramsKey: map[string]any{
			binaryKey: false,
		},
	}
	return a.SubscribePublic(ctx, sym+":depth", msg)
}

// UnsubscribeDepth stops streaming spot orderbook depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol string) error {
	sym := formatSpotSymbol(symbol)
	msg := map[string]any{
		eventKey:  eventCancel,
		topicKey:  depthKey,
		symbolKey: sym,
		paramsKey: map[string]any{
			binaryKey: false,
		},
	}
	return a.UnsubscribePublic(ctx, sym+":depth", msg)
}

// SubscribeTrade streams spot public trades.
func (a *WsAdapter) SubscribeTrade(ctx context.Context, symbol string) error {
	sym := formatSpotSymbol(symbol)
	msg := map[string]any{
		eventKey:  eventSub,
		topicKey:  tradeKey,
		symbolKey: sym,
		paramsKey: map[string]any{
			binaryKey: false,
		},
	}
	return a.SubscribePublic(ctx, sym+":trade", msg)
}

// UnsubscribeTrade stops streaming spot public trades.
func (a *WsAdapter) UnsubscribeTrade(ctx context.Context, symbol string) error {
	sym := formatSpotSymbol(symbol)
	msg := map[string]any{
		eventKey:  eventCancel,
		topicKey:  tradeKey,
		symbolKey: sym,
		paramsKey: map[string]any{
			binaryKey: false,
		},
	}
	return a.UnsubscribePublic(ctx, sym+":trade", msg)
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
			if msg.Topic == tradeKey || msg.Event == tradeKey {
				return tradeKey
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
		return "", nil, fmt.Errorf("unmarshal depth basic: %w", err)
	}

	sym := basic.Symbol

	var entries []wsDepthEntry
	if err := xjson.Unmarshal(basic.Data, &entries); err != nil {
		var single wsDepthEntry
		if err2 := xjson.Unmarshal(basic.Data, &single); err2 != nil {
			return "", nil, fmt.Errorf("unmarshal depth data: %w", err)
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

	ts := time.Now().UTC()
	if latest.T > 0 {
		ts = time.UnixMilli(latest.T).UTC()
	}

	return sym, &domain.OrderBook{
		Symbol:    sym,
		Version:   parseToobitDepthVersion(latest.V),
		Timestamp: ts,
		Bids:      bids,
		Asks:      asks,
	}, nil
}

type wsTradeEntry struct {
	Price        xjson.Number `json:"p"`
	Quantity     xjson.Number `json:"q"`
	TradeID      string       `json:"v"`
	IsBuyerMaker bool         `json:"m"`
	Time         int64        `json:"t"`
}

func parseTradeEntries(raw json.RawMessage) ([]wsTradeEntry, error) {
	var entries []wsTradeEntry
	if err := xjson.Unmarshal(raw, &entries); err == nil {
		return entries, nil
	}
	var single wsTradeEntry
	if err := xjson.Unmarshal(raw, &single); err == nil {
		return []wsTradeEntry{single}, nil
	}
	return nil, fmt.Errorf("unmarshal trade data failed")
}

func parseTakerSide(e wsTradeEntry) domain.Side {
	if e.IsBuyerMaker {
		return domain.SideOpenShort
	}
	return domain.SideOpenLong
}

// ParseTrade parses public trade messages into []domain.PublicTrade.
func (a *WsAdapter) ParseTrade(data []byte) (string, []domain.PublicTrade, error) {
	var basic struct {
		Topic  string          `json:"topic"`
		Symbol string          `json:"symbol"`
		Data   json.RawMessage `json:"data"`
	}
	if err := xjson.Unmarshal(data, &basic); err != nil {
		return "", nil, fmt.Errorf("unmarshal trade basic: %w", err)
	}

	sym := basic.Symbol
	entries, err := parseTradeEntries(basic.Data)
	if err != nil {
		return "", nil, fmt.Errorf("unmarshal trade data: %w", err)
	}

	if len(entries) == 0 {
		return sym, nil, nil
	}

	trades := make([]domain.PublicTrade, 0, len(entries))
	for _, e := range entries {
		p, _ := e.Price.Float64()
		v, _ := e.Quantity.Float64()
		if p <= 0 || v <= 0 {
			continue
		}

		ts := time.Now().UTC()
		if e.Time > 0 {
			ts = time.UnixMilli(e.Time).UTC()
		}

		trades = append(trades, domain.PublicTrade{
			Symbol:    sym,
			Price:     p,
			Volume:    v,
			Side:      parseTakerSide(e),
			Timestamp: ts,
		})
	}

	return sym, trades, nil
}

// ParseTicker is a no-op for spot in this phase.
func (a *WsAdapter) ParseTicker(data []byte) (string, *store.PriceData, error) {
	return "", nil, nil
}

// ParsePosition is a no-op for spot.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, nil
}
