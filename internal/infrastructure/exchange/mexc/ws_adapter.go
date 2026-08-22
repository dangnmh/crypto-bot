package mexc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"

	"crypto-bot/pkg/xjson"
)

var (
	_ exchange.DepthSubscriber = (*WsAdapter)(nil)
	_ exchange.DepthParser     = (*WsAdapter)(nil)
)

// WsAdapter implements ws.ExchangeAdapter for MEXC Futures.
type WsAdapter struct {
	pool          *pkgws.Pool
	authenticated chan struct{}
	authMu        sync.Mutex
}

// NewWsAdapter creates a new MEXC WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{
		authenticated: make(chan struct{}),
	}
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

// SubscribeTicker subscribes to ticker push.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: "sub.ticker",
		paramParam:  map[string]string{paramSymbol: symbol},
	}
	topic := symbol + ":" + channelTicker
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from ticker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: "unsub.ticker",
		paramParam:  map[string]string{paramSymbol: symbol},
	}
	topic := symbol + ":" + channelTicker
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeDepth subscribes to Level 2 incremental orderbook depth updates (push.depth).
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: "sub.depth",
		paramParam: map[string]any{
			paramSymbol: symbol,
		},
	}
	topic := symbol + ":" + channelDepth
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from Level 2 incremental orderbook depth updates.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: "unsub.depth",
		paramParam: map[string]any{
			paramSymbol: symbol,
		},
	}
	topic := symbol + ":" + channelDepth
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to all private futures channels used by funding flows.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	a.authMu.Lock()
	authCh := a.authenticated
	a.authMu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-authCh:
	}

	msg := map[string]any{
		paramMethod: "personal.filter",
		paramParam: map[string]any{
			"filters": []map[string]string{
				{paramFilter: "order"},
				{paramFilter: "order.deal"},
				{paramFilter: "track.order"},
				{paramFilter: "position"},
			},
		},
	}
	return a.pool.SendPrivate(ctx, msg)
}

func (a *WsAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
}

// GetPingConfig returns the ping payload and interval for MEXC.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return map[string]string{paramMethod: "ping"}, 15 * time.Second
}

// GetAuthHook returns the OnConnected hook for MEXC authentication.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	if apiKey == "" {
		a.authMu.Lock()
		select {
		case <-a.authenticated:
		default:
			close(a.authenticated)
		}
		a.authMu.Unlock()
		return nil
	}
	return func(c *pkgws.Client) {
		a.authMu.Lock()
		a.authenticated = make(chan struct{})
		a.authMu.Unlock()

		reqTime := fmt.Sprintf("%d", time.Now().UnixMilli())
		message := apiKey + reqTime
		mac := hmac.New(sha256.New, []byte(apiSecret))
		mac.Write([]byte(message))
		signature := hex.EncodeToString(mac.Sum(nil))

		msg := map[string]any{
			paramMethod: opLogin,
			paramParam: map[string]any{
				"apiKey":    apiKey,
				"reqTime":   reqTime,
				"signature": signature,
				"subscribe": false,
			},
		}
		_ = c.SendJSON(msg)
	}
}

func mapMexcChannel(channel string) string {
	switch channel {
	case "push.ticker":
		return channelTicker
	case "push.depth", "push.depth.full", "push.depth.step":
		return channelDepth
	case "push.kline":
		return channelKline
	case "push.personal.order":
		return "personal.order"
	case "push.personal.order.deal":
		return "personal.order.deal"
	case "push.personal.track.order":
		return "personal.track.order"
	case "push.personal.position":
		return "personal.position"
	default:
		if after, ok := strings.CutPrefix(channel, "push."); ok {
			return after
		}
		return channel
	}
}

// GetChannelExtractor returns the function that maps raw MEXC messages to generic internal channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		channel, err := jsonparser.GetString(data, "channel")
		if err == nil && channel == "rs.login" {
			mexcData, _ := jsonparser.GetString(data, "data")
			if mexcData == "success" {
				a.authMu.Lock()
				select {
				case <-a.authenticated:
				default:
					close(a.authenticated)
				}
				a.authMu.Unlock()
			}
			return opLogin
		}

		var baseMsg struct {
			Channel string `json:"channel"`
		}
		if err := xjson.Unmarshal(data, &baseMsg); err == nil {
			return mapMexcChannel(baseMsg.Channel)
		}
		return ""
	}
}

// WsTickerData represents MEXC's ticker format.
type WsTickerData struct {
	Symbol      string  `json:"symbol"`
	LastPrice   float64 `json:"lastPrice"`
	FairPrice   float64 `json:"fairPrice"`
	IndexPrice  float64 `json:"indexPrice"`
	Volume24    float64 `json:"volume24"`
	Amount24    float64 `json:"amount24"`
	MaxBidPrice float64 `json:"maxBidPrice"`
	MinAskPrice float64 `json:"minAskPrice"`
	Timestamp   int64   `json:"timestamp"`
	Bid1        float64 `json:"bid1"`
	Ask1        float64 `json:"ask1"`
}

// ParseTicker parses raw JSON into generic store.PriceData.
func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	var msg struct {
		Symbol string          `json:"symbol"`
		Data   json.RawMessage `json:"data"`
	}
	if err = xjson.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}

	var ticker WsTickerData
	if err = xjson.Unmarshal(msg.Data, &ticker); err != nil {
		return "", nil, err
	}

	pd = &store.PriceData{
		Symbol:    msg.Symbol,
		LastPrice: ticker.LastPrice,
		BestBid:   ticker.Bid1,
		BestAsk:   ticker.Ask1,
		FairPrice: ticker.FairPrice,
		Volume24:  ticker.Volume24,
		UpdatedAt: time.Now(),
	}

	if pd.BestBid == 0 && ticker.MaxBidPrice > 0 {
		pd.BestBid = ticker.MaxBidPrice
	}
	if pd.BestAsk == 0 && ticker.MinAskPrice > 0 {
		pd.BestAsk = ticker.MinAskPrice
	}

	return msg.Symbol, pd, nil
}

// ParsePosition parses push.personal.position into position exposure data.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg struct {
		Data json.RawMessage `json:"data"`
	}
	if err := xjson.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	type updateAlias exchange.PersonalPositionUpdate
	var raw struct {
		updateAlias
		HoldVol  float64 `json:"holdVol"`
		CloseVol float64 `json:"closeVol"`
	}

	if err := xjson.Unmarshal(msg.Data, &raw); err != nil {
		return nil, err
	}

	update := exchange.PersonalPositionUpdate(raw.updateAlias)
	if update.HoldVolContract == 0 && raw.HoldVol > 0 {
		update.HoldVolContract = raw.HoldVol
	}
	if update.CloseVolContract == 0 && raw.CloseVol > 0 {
		update.CloseVolContract = raw.CloseVol
	}

	return &update, nil
}

type wsMexcDepthData struct {
	Begin   int64            `json:"begin"`
	End     int64            `json:"end"`
	Version int64            `json:"version"`
	Bids    [][]xjson.Number `json:"bids"`
	Asks    [][]xjson.Number `json:"asks"`
}

// ParseDepth parses MEXC depth messages into domain.OrderBook.
func (a *WsAdapter) ParseDepth(data []byte) (string, *domain.OrderBook, error) {
	var msg struct {
		Symbol  string          `json:"symbol"`
		Channel string          `json:"channel"`
		Data    json.RawMessage `json:"data"`
	}
	if err := xjson.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}

	var depthData wsMexcDepthData
	if err := xjson.Unmarshal(msg.Data, &depthData); err != nil {
		return "", nil, err
	}

	sym := msg.Symbol
	firstVer := depthData.Begin
	endVer := depthData.End
	if endVer == 0 {
		endVer = depthData.Version
	}
	if firstVer == 0 {
		firstVer = endVer
	}

	bids := make([]domain.OrderBookEntry, 0, len(depthData.Bids))
	for _, b := range depthData.Bids {
		if len(b) >= 2 {
			p := xjson.ToFloat64(b[0])
			v := xjson.ToFloat64(b[1])
			if len(b) >= 3 {
				v = xjson.ToFloat64(b[2]) // Index 2 is contract quantity
			}
			if p > 0 {
				bids = append(bids, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	asks := make([]domain.OrderBookEntry, 0, len(depthData.Asks))
	for _, a := range depthData.Asks {
		if len(a) >= 2 {
			p := xjson.ToFloat64(a[0])
			v := xjson.ToFloat64(a[1])
			if len(a) >= 3 {
				v = xjson.ToFloat64(a[2]) // Index 2 is contract quantity
			}
			if p > 0 {
				asks = append(asks, domain.OrderBookEntry{Price: p, Volume: v})
			}
		}
	}

	return sym, &domain.OrderBook{
		Symbol:       sym,
		FirstVersion: firstVer,
		Version:      endVer,
		Bids:         bids,
		Asks:         asks,
	}, nil
}
