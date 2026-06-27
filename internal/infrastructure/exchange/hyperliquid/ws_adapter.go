package hyperliquid

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"
	"github.com/ethereum/go-ethereum/crypto"
)

// WsAdapter implements ws.ExchangeAdapter for Hyperliquid WebSocket.
type WsAdapter struct {
	pool        *pkgws.Pool
	userAddress string
}

// NewWsAdapter creates a new WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{}
}

// SetPool injects the websocket connection pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// SubscribeTicker subscribes to mid prices using allMids channel.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: opSubscribe,
		fieldSubscription: map[string]any{
			fieldType: channelAllMids,
		},
	}
	topic := symbol + ":tickers"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from mid prices.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		paramMethod: opUnsubscribe,
		fieldSubscription: map[string]any{
			fieldType: channelAllMids,
		},
	}
	topic := symbol + ":tickers"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to user events (fills, orders).
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	if a.userAddress == "" {
		return fmt.Errorf("userAddress is not initialized for WS auth")
	}

	msg := map[string]any{
		paramMethod: opSubscribe,
		fieldSubscription: map[string]any{
			fieldType: "userEvents",
			"user":    a.userAddress,
		},
	}
	return a.pool.SendPrivate(ctx, msg)
}

// GetPingConfig returns application ping and interval.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return map[string]any{
		"method": "ping",
	}, 30 * time.Second
}

// GetAuthHook intercepts OnConnected to derive userAddress.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	if apiSecret != "" {
		pk, err := crypto.HexToECDSA(strings.TrimPrefix(apiSecret, "0x"))
		if err == nil {
			a.userAddress = crypto.PubkeyToAddress(pk.PublicKey).Hex()
		}
	}
	return nil
}

// GetChannelExtractor routes incoming WS channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		if string(data) == `"pong"` || string(data) == "pong" {
			return msgPong
		}

		channel, err := jsonparser.GetString(data, "channel")
		if err != nil {
			return ""
		}

		switch channel {
		case channelAllMids:
			return "ticker"
		case channelL2Book:
			return "depth"
		case channelCandle:
			return "kline"
		case channelUser:
			return "personal.order"
		}
		return channel
	}
}

// ParseTicker parses allMids push.
func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	midsBytes, _, _, err := jsonparser.Get(data, "data", "mids")
	if err != nil {
		return "", nil, err
	}

	var parsedSymbol string
	var price float64
	_ = jsonparser.ObjectEach(midsBytes, func(key []byte, value []byte, dataType jsonparser.ValueType, offset int) error {
		parsedSymbol = string(key)
		price, _ = strconv.ParseFloat(string(value), 64)
		return fmt.Errorf("stop")
	})

	if parsedSymbol == "" {
		return "", nil, fmt.Errorf("no mids found")
	}

	pd = &store.PriceData{
		Symbol:    parsedSymbol,
		LastPrice: price,
		BestBid:   price,
		BestAsk:   price,
		FairPrice: price,
		Volume24:  0,
		UpdatedAt: time.Now(),
	}
	return parsedSymbol, pd, nil
}

// ParseDepthCommit is a stub.
func (a *WsAdapter) ParseDepthCommit(data []byte) (symbol string, commit *exchange.DepthCommit, err error) {
	return "", nil, nil
}

// ParsePosition is a stub.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, nil
}
