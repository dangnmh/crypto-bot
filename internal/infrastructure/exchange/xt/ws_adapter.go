package xt

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	infraws "crypto-bot/internal/infrastructure/ws"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"
)

// WsAdapter implements ws.ExchangeAdapter for XT.com.
type WsAdapter struct {
	pool       *pkgws.Pool
	client     *Client
	apiKey     string
	apiSecret  string
	priceCache *infraws.PriceCache
	listenKey  string
}

// NewWsAdapter creates a new WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{
		priceCache: infraws.NewPriceCache(),
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

// SetClient injects the REST client reference.
func (a *WsAdapter) SetClient(client *Client) {
	a.client = client
}

func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	sym := cleanXTSymbol(symbol)

	topic := fmt.Sprintf("agg_ticker@%s", sym)
	msg := map[string]any{
		paramMethod: opSubscribe,
		paramParams: []string{topic},
		"id":        time.Now().UnixMilli(),
	}

	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker stops streaming ticker updates.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	sym := cleanXTSymbol(symbol)

	topic := fmt.Sprintf("agg_ticker@%s", sym)
	msg := map[string]any{
		paramMethod: "UNSUBSCRIBE",
		paramParams: []string{topic},
		"id":        time.Now().UnixMilli(),
	}

	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal streams private events (order/position updates) using listenKey.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	listenKey, err := a.client.GetListenKey(ctx)
	if err != nil {
		return fmt.Errorf("failed to get listenKey: %w", err)
	}

	a.listenKey = listenKey

	msg := map[string]any{
		paramMethod: opSubscribe,
		paramParams: []string{
			fmt.Sprintf("position@%s", listenKey),
		},
		"id": time.Now().UnixMilli(),
	}

	return a.pool.SendPrivate(ctx, msg)
}

func (a *WsAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
}

// GetPingConfig returns ping parameters.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return "ping", 20 * time.Second
}

// GetAuthHook sets private credentials.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	a.apiKey = apiKey
	a.apiSecret = apiSecret
	return nil
}

// GetChannelExtractor routes WebSocket push channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		var msg struct {
			Topic string `json:"topic"`
			Event string `json:"event"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			return ""
		}
		topic := msg.Topic
		if topic == "" && strings.Contains(msg.Event, "@") {
			parts := strings.Split(msg.Event, "@")
			topic = parts[0]
		}
		switch topic {
		case "agg_ticker", "ticker":
			return channelTicker
		case "position":
			return channelPosition
		case "order":
			return channelOrder
		}
		return topic
	}
}

// ParseTicker parses raw JSON ticker update.
func (a *WsAdapter) ParseTicker(data []byte) (string, *store.PriceData, error) {
	var msg struct {
		Topic string `json:"topic"`
		Event string `json:"event"`
		Data  struct {
			T  int64  `json:"t"`
			S  string `json:"s"`
			C  string `json:"c"`
			A  string `json:"a"`
			Bp string `json:"bp"`
			Ap string `json:"ap"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return "", nil, fmt.Errorf("unmarshal ws ticker: %w", err)
	}

	stdSym := toStandardSymbol(msg.Data.S)
	price, _ := strconv.ParseFloat(msg.Data.C, 64)
	vol, _ := strconv.ParseFloat(msg.Data.A, 64)

	a.priceCache.UpdateTicker(stdSym, price, price, vol)
	var pd *store.PriceData
	if msg.Data.Bp != "" && msg.Data.Ap != "" {
		bid, _ := strconv.ParseFloat(msg.Data.Bp, 64)
		ask, _ := strconv.ParseFloat(msg.Data.Ap, 64)
		pd = a.priceCache.UpdateDepth(stdSym, bid, ask)
	} else {
		pd = a.priceCache.UpdateDepth(stdSym, price, price)
	}

	return stdSym, pd, nil
}

// ParsePosition parses raw JSON position update.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg struct {
		Topic string `json:"topic"`
		Event string `json:"event"`
		Data  struct {
			Symbol       string       `json:"symbol"`
			PositionSide string       `json:"positionSide"`
			PositionSize xjson.Number `json:"positionSize"`
			EntryPrice   xjson.Number `json:"entryPrice"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal ws position: %w", err)
	}

	stdSym := toStandardSymbol(msg.Data.Symbol)
	qty := xjson.ToFloat64(msg.Data.PositionSize)
	entryPrice := xjson.ToFloat64(msg.Data.EntryPrice)

	posType := exchange.PositionTypeLong
	if strings.EqualFold(msg.Data.PositionSide, sideShort) {
		posType = exchange.PositionTypeShort
	}

	return &exchange.PersonalPositionUpdate{
		Symbol:          stdSym,
		HoldVolContract: qty,
		PositionType:    posType,
		OpenAvgPrice:    entryPrice,
		HoldAvgPrice:    entryPrice,
		UpdateTime:      time.Now().UnixMilli(),
	}, nil
}
