package bingx

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"
)

// WsAdapter implements ws.ExchangeAdapter for BingX Futures.
type WsAdapter struct {
	pool *pkgws.Pool
}

// NewWsAdapter creates a new BingX WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{}
}

// SetPool injects the websocket pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// SubscribeTicker subscribes to ticker stream.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	topic := symbol + "@" + channelTicker
	msg := map[string]interface{}{
		"id":          "sub-" + symbol + "-ticker",
		paramReqType:  opSub,
		paramDataType: topic,
	}
	return a.pool.SubscribePublic(ctx, symbol+":tickers", msg)
}

// UnsubscribeTicker unsubscribes from ticker stream.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	topic := symbol + "@" + channelTicker
	msg := map[string]interface{}{
		"id":          "unsub-" + symbol + "-ticker",
		paramReqType:  opUnsub,
		paramDataType: topic,
	}
	return a.pool.UnsubscribePublic(ctx, symbol+":tickers", msg)
}

// SubscribeKline subscribes to 1-minute klines.
func (a *WsAdapter) SubscribeKline(ctx context.Context, symbol string) error {
	topic := symbol + "@" + channelKline + "_1m"
	msg := map[string]interface{}{
		"id":          "sub-" + symbol + "-kline",
		paramReqType:  opSub,
		paramDataType: topic,
	}
	return a.pool.SubscribePublic(ctx, symbol+":kline", msg)
}

// UnsubscribeKline unsubscribes from klines.
func (a *WsAdapter) UnsubscribeKline(ctx context.Context, symbol string) error {
	topic := symbol + "@" + channelKline + "_1m"
	msg := map[string]interface{}{
		"id":          "unsub-" + symbol + "-kline",
		paramReqType:  opUnsub,
		paramDataType: topic,
	}
	return a.pool.UnsubscribePublic(ctx, symbol+":kline", msg)
}

// SubscribeDepth subscribes to depth.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol, step string) error {
	topic := symbol + "@" + channelDepth + "20"
	msg := map[string]interface{}{
		"id":          "sub-" + symbol + "-depth",
		paramReqType:  opSub,
		paramDataType: topic,
	}
	return a.pool.SubscribePublic(ctx, symbol+":depth", msg)
}

// UnsubscribeDepth unsubscribes from depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol, step string) error {
	topic := symbol + "@" + channelDepth + "20"
	msg := map[string]interface{}{
		"id":          "unsub-" + symbol + "-depth",
		paramReqType:  opUnsub,
		paramDataType: topic,
	}
	return a.pool.UnsubscribePublic(ctx, symbol+":depth", msg)
}

// SubscribePersonal is a placeholder since we only scan public channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	return nil
}

// GetPingConfig returns application ping config.
func (a *WsAdapter) GetPingConfig() (interface{}, time.Duration) {
	return msgPing, 30 * time.Second
}

// GetAuthHook is not used for BingX Futures since we don't stream personal deals.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	return nil
}

// GetPreprocessor returns decompression function for GZIP payloads.
func (a *WsAdapter) GetPreprocessor() func([]byte) ([]byte, error) {
	return func(data []byte) ([]byte, error) {
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer func() { _ = r.Close() }()
		return io.ReadAll(r)
	}
}

// GetChannelExtractor maps BingX events to channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		if string(data) == msgPing {
			return msgPing
		}

		dataType, err := jsonparser.GetString(data, paramDataType)
		if err != nil {
			return ""
		}

		parts := strings.Split(dataType, "@")
		if len(parts) < 2 {
			return ""
		}

		streamType := parts[1]
		if strings.HasPrefix(streamType, channelKline) {
			return channelKline
		}
		if strings.HasPrefix(streamType, channelDepth) {
			return channelDepth
		}
		if streamType == channelTicker {
			return "tickers"
		}

		return dataType
	}
}

// ParseTicker parses ticker feed into store.PriceData.
func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	dataType, err := jsonparser.GetString(data, paramDataType)
	if err != nil {
		return "", nil, err
	}

	parts := strings.Split(dataType, "@")
	if len(parts) < 1 {
		return "", nil, fmt.Errorf("invalid dataType in ticker push: %s", dataType)
	}
	sym := parts[0]

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	type wsTicker struct {
		LastPrice string `json:"lastPrice"`
		BidPrice  string `json:"bidPrice"`
		AskPrice  string `json:"askPrice"`
		Volume    string `json:"volume"`
	}

	var raw wsTicker
	if err := json.Unmarshal(dataNode, &raw); err != nil {
		return "", nil, fmt.Errorf("unmarshal ticker push: %w", err)
	}

	last := decmath.ParseFloat(raw.LastPrice)
	bid := decmath.ParseFloat(raw.BidPrice)
	ask := decmath.ParseFloat(raw.AskPrice)
	vol := decmath.ParseFloat(raw.Volume)

	pd = &store.PriceData{
		Symbol:    sym,
		LastPrice: last,
		BestBid:   bid,
		BestAsk:   ask,
		FairPrice: last,
		Volume24:  vol,
		UpdatedAt: time.Now(),
	}

	return sym, pd, nil
}

// ParseDepth parses books feed into domain.OrderBook.
func (a *WsAdapter) ParseDepth(data []byte) (symbol string, ob *domain.OrderBook, err error) {
	dataType, err := jsonparser.GetString(data, paramDataType)
	if err != nil {
		return "", nil, err
	}

	parts := strings.Split(dataType, "@")
	if len(parts) < 1 {
		return "", nil, fmt.Errorf("invalid dataType in depth push: %s", dataType)
	}
	sym := parts[0]

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	type wsDepth struct {
		Asks [][]string `json:"asks"`
		Bids [][]string `json:"bids"`
	}

	var raw wsDepth
	if err := json.Unmarshal(dataNode, &raw); err != nil {
		return "", nil, fmt.Errorf("unmarshal depth push: %w", err)
	}

	book := &domain.OrderBook{
		Symbol: sym,
		Asks:   make([]domain.OrderBookEntry, 0, len(raw.Asks)),
		Bids:   make([]domain.OrderBookEntry, 0, len(raw.Bids)),
	}

	for _, level := range raw.Asks {
		if len(level) < 2 {
			continue
		}
		book.Asks = append(book.Asks, domain.OrderBookEntry{
			Price:  decmath.ParseFloat(level[0]),
			Volume: decmath.ParseFloat(level[1]),
		})
	}

	for _, level := range raw.Bids {
		if len(level) < 2 {
			continue
		}
		book.Bids = append(book.Bids, domain.OrderBookEntry{
			Price:  decmath.ParseFloat(level[0]),
			Volume: decmath.ParseFloat(level[1]),
		})
	}

	return sym, book, nil
}

// ParseKline is a placeholder.
func (a *WsAdapter) ParseKline(data []byte) (symbol string, k *domain.Kline, err error) {
	return "", nil, fmt.Errorf("ParseKline not implemented on BingX WS")
}

// ParseOrder is a placeholder.
func (a *WsAdapter) ParseOrder(data []byte) (*exchange.WsOrderDeal, error) {
	return nil, fmt.Errorf("ParseOrder not implemented on BingX WS")
}

// ParseOrderDeal is a placeholder.
func (a *WsAdapter) ParseOrderDeal(data []byte) (*exchange.PersonalOrderDeal, error) {
	return nil, fmt.Errorf("ParseOrderDeal not implemented on BingX WS")
}

// ParseTrackOrder is a placeholder.
func (a *WsAdapter) ParseTrackOrder(data []byte) (*exchange.PersonalTrackOrderUpdate, error) {
	return nil, fmt.Errorf("ParseTrackOrder not implemented on BingX WS")
}

// ParsePosition is a placeholder.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, fmt.Errorf("ParsePosition not implemented on BingX WS")
}
