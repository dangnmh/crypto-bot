package futures

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"

	"github.com/buger/jsonparser"
)

const (
	paramKey  = "param"
	filterKey = "filter"
	methodKey = "method"
	symbolKey = "symbol"
	opLogin   = "login"
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

// NewWsAdapter creates a new MEXC Futures WsAdapter.
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
		methodKey: "sub.ticker",
		paramKey:  map[string]string{symbolKey: symbol},
	}
	topic := symbol + ":ticker"
	return a.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from ticker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		methodKey: "unsub.ticker",
		paramKey:  map[string]string{symbolKey: symbol},
	}
	topic := symbol + ":ticker"
	return a.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeDepth subscribes to Level 2 orderbook depth updates.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol string) error {
	msg := map[string]any{
		methodKey: "sub.depth",
		paramKey: map[string]any{
			symbolKey: symbol,
		},
	}
	topic := symbol + ":depth"
	return a.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from Level 2 orderbook depth updates.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol string) error {
	msg := map[string]any{
		methodKey: "unsub.depth",
		paramKey: map[string]any{
			symbolKey: symbol,
		},
	}
	topic := symbol + ":depth"
	return a.UnsubscribePublic(ctx, topic, msg)
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
		methodKey: "personal.filter",
		paramKey: map[string]any{
			"filters": []map[string]string{
				{filterKey: "order"},
				{filterKey: "order.deal"},
				{filterKey: "track.order"},
				{filterKey: "position"},
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
	return map[string]string{methodKey: "ping"}, 15 * time.Second
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
			methodKey: opLogin,
			paramKey: map[string]any{
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
		return "ticker"
	case "push.depth", "push.depth.full", "push.depth.step":
		return "depth"
	case "push.kline":
		return "kline"
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

// GetChannelExtractor extracts routing information for incoming messages.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(message []byte) string {
		channelVal, err := jsonparser.GetString(message, "channel")
		if err == nil {
			if channelVal == "rs.login" || channelVal == opLogin {
				a.authMu.Lock()
				select {
				case <-a.authenticated:
				default:
					close(a.authenticated)
				}
				a.authMu.Unlock()
				return opLogin
			}
			return mapMexcChannel(channelVal)
		}

		code, err := jsonparser.GetInt(message, "code")
		if err == nil && code == 0 {
			a.authMu.Lock()
			select {
			case <-a.authenticated:
			default:
				close(a.authenticated)
			}
			a.authMu.Unlock()
			return "auth"
		}

		return ""
	}
}

// ParseTicker parses a ticker push message.
func (a *WsAdapter) ParseTicker(message []byte) (string, *store.PriceData, error) {
	dataBytes, dataType, _, err := jsonparser.Get(message, "data")
	if err != nil || dataType != jsonparser.Object {
		return "", nil, fmt.Errorf("invalid ticker data format")
	}

	symbol, _ := jsonparser.GetString(message, "symbol")
	if symbol == "" {
		symbol, _ = jsonparser.GetString(dataBytes, "symbol")
	}
	if symbol == "" {
		return "", nil, fmt.Errorf("missing symbol in ticker payload")
	}

	lastPrice, _ := jsonparser.GetFloat(dataBytes, "lastPrice")
	bid1, _ := jsonparser.GetFloat(dataBytes, "bid1")
	ask1, _ := jsonparser.GetFloat(dataBytes, "ask1")
	fairPrice, _ := jsonparser.GetFloat(dataBytes, "fairPrice")
	maxBidPrice, _ := jsonparser.GetFloat(dataBytes, "maxBidPrice")
	minAskPrice, _ := jsonparser.GetFloat(dataBytes, "minAskPrice")
	volume24, _ := jsonparser.GetFloat(dataBytes, "volume24")

	if bid1 == 0 && maxBidPrice > 0 {
		bid1 = maxBidPrice
	}
	if ask1 == 0 && minAskPrice > 0 {
		ask1 = minAskPrice
	}

	pd := &store.PriceData{
		Symbol:    symbol,
		LastPrice: lastPrice,
		BestBid:   bid1,
		BestAsk:   ask1,
		FairPrice: fairPrice,
		Volume24:  volume24,
		UpdatedAt: time.Now(),
	}

	return symbol, pd, nil
}

// ParseDepth parses a Level 2 depth push message into domain.OrderBook.
func (a *WsAdapter) ParseDepth(message []byte) (string, *domain.OrderBook, error) {
	symbol, err := jsonparser.GetString(message, "symbol")
	if err != nil {
		return "", nil, fmt.Errorf("missing symbol in depth payload: %w", err)
	}

	beginVer, _ := jsonparser.GetInt(message, "data", "begin")
	endVer, _ := jsonparser.GetInt(message, "data", "end")
	if endVer == 0 {
		endVer, _ = jsonparser.GetInt(message, "data", "version")
	}
	if beginVer == 0 {
		beginVer = endVer
	}

	parseLevels := func(key string) ([]domain.OrderBookEntry, error) {
		rawLevels, dataType, _, getErr := jsonparser.Get(message, "data", key)
		if getErr != nil {
			if errors.Is(getErr, jsonparser.KeyPathNotFoundError) {
				return nil, nil
			}
			return nil, fmt.Errorf("read %s in depth payload: %w", key, getErr)
		}
		if dataType != jsonparser.Array {
			return nil, nil
		}

		var levels [][]xjson.Number
		if unmarshalErr := xjson.Unmarshal(rawLevels, &levels); unmarshalErr != nil {
			return nil, fmt.Errorf("failed to unmarshal %s: %w", key, unmarshalErr)
		}

		res := make([]domain.OrderBookEntry, 0, len(levels))
		for _, item := range levels {
			if len(item) < 2 {
				continue
			}
			p := xjson.ToFloat64(item[0])
			v := xjson.ToFloat64(item[1])
			if len(item) >= 3 {
				v = xjson.ToFloat64(item[2]) // Index 2 is contract quantity
			}
			if p <= 0 {
				continue
			}
			res = append(res, domain.OrderBookEntry{
				Price:  p,
				Volume: v,
			})
		}
		return res, nil
	}

	bids, err := parseLevels("bids")
	if err != nil {
		return symbol, nil, err
	}
	asks, err := parseLevels("asks")
	if err != nil {
		return symbol, nil, err
	}

	return symbol, &domain.OrderBook{
		Symbol:       symbol,
		FirstVersion: beginVer,
		Version:      endVer,
		Bids:         bids,
		Asks:         asks,
	}, nil
}

// ParsePosition parses a position push message.
func (a *WsAdapter) ParsePosition(message []byte) (*exchange.PersonalPositionUpdate, error) {
	dataBytes, dataType, _, err := jsonparser.Get(message, "data")
	if err != nil || dataType != jsonparser.Object {
		return nil, fmt.Errorf("invalid position data format")
	}

	var raw mexcPosition
	if err := json.Unmarshal(dataBytes, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal mexc position push: %w", err)
	}
	pos := raw.toPosition()
	return &exchange.PersonalPositionUpdate{
		Symbol:          pos.Symbol,
		HoldVolContract: pos.HoldVolContract,
		HoldVolCoin:     pos.HoldVolCoin,
		PositionType:    pos.PositionType,
		OpenAvgPrice:    pos.OpenAvgPrice,
		HoldAvgPrice:    pos.HoldAvgPrice,
		CloseProfitLoss: raw.Realised,
		Leverage:        pos.Leverage,
		LiquidatePrice:  raw.LiquidatePrice,
		UpdateTime:      raw.UpdateTime,
	}, nil
}
