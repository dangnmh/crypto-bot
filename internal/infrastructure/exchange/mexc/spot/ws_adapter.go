package spot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	mexcproto "crypto-bot/internal/infrastructure/exchange/mexc/spot/proto"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
)

const (
	opSubscription   = "SUBSCRIPTION"
	opUnsubscription = "UNSUBSCRIPTION"
	paramsKey        = "params"
	methodKey        = "method"
	depthStream100ms = "spot@public.aggre.depth.v3.api.pb@100ms@"
	bookTickerStream = "spot@public.bookTicker.v3.api.pb@"
	channelDepth     = "depth"
	channelTicker    = "ticker"
)

var (
	_ exchange.DepthSubscriber = (*WsAdapter)(nil)
	_ exchange.DepthParser     = (*WsAdapter)(nil)
)

// WsAdapter implements ws.ExchangeAdapter for MEXC Spot WebSocket.
type WsAdapter struct {
	pool *pkgws.Pool
}

// NewWsAdapter creates a new MEXC Spot WsAdapter.
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

// SubscribeTicker subscribes to spot bookTicker push.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	stream := bookTickerStream + symbol
	msg := map[string]any{
		methodKey: opSubscription,
		paramsKey: []string{stream},
	}
	topic := symbol + ":ticker"
	return a.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from spot bookTicker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	stream := bookTickerStream + symbol
	msg := map[string]any{
		methodKey: opUnsubscription,
		paramsKey: []string{stream},
	}
	topic := symbol + ":ticker"
	return a.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeDepth subscribes to spot aggregated/diff depth updates (100ms).
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol string) error {
	stream := depthStream100ms + symbol
	msg := map[string]any{
		methodKey: opSubscription,
		paramsKey: []string{stream},
	}
	topic := symbol + ":depth"
	return a.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from spot aggregated/diff depth updates.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol string) error {
	stream := depthStream100ms + symbol
	msg := map[string]any{
		methodKey: opUnsubscription,
		paramsKey: []string{stream},
	}
	topic := symbol + ":depth"
	return a.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal is a no-op for spot in this phase.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	return nil
}

func (a *WsAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
}

// GetPingConfig returns the ping payload and interval for MEXC Spot WS.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return map[string]string{methodKey: "PING"}, 15 * time.Second
}

// GetCustomPingHandler returns a custom handler for MEXC Spot WS PING/PONG frames.
func (a *WsAdapter) GetCustomPingHandler() func(*websocket.Conn, []byte) bool {
	return func(conn *websocket.Conn, data []byte) bool {
		msgStr, err := jsonparser.GetString(data, "msg")
		if err == nil && strings.EqualFold(msgStr, "PONG") {
			return true
		}
		method, err := jsonparser.GetString(data, methodKey)
		if err == nil && strings.EqualFold(method, "PING") {
			_ = conn.WriteJSON(map[string]string{methodKey: "PONG"})
			return true
		}
		return false
	}
}

// GetAuthHook returns nil for public spot streams.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	return nil
}

func extractProtoChannel(message []byte) string {
	var wrapper mexcproto.PushDataV3ApiWrapper
	if err := proto.Unmarshal(message, &wrapper); err != nil || (wrapper.GetChannel() == "" && wrapper.GetBody() == nil) {
		return ""
	}
	channel := wrapper.GetChannel()
	if strings.Contains(channel, channelDepth) || wrapper.GetPublicAggreDepths() != nil || wrapper.GetPublicIncreaseDepths() != nil || wrapper.GetPublicLimitDepths() != nil {
		return channelDepth
	}
	if strings.Contains(channel, "bookTicker") || strings.Contains(channel, channelTicker) || wrapper.GetPublicBookTicker() != nil {
		return channelTicker
	}
	return channel
}

func extractJSONChannel(message []byte) string {
	channel, err := jsonparser.GetString(message, "channel")
	if err != nil {
		channel, _ = jsonparser.GetString(message, "c")
	}
	if channel != "" {
		if strings.Contains(channel, channelDepth) {
			return channelDepth
		}
		if strings.Contains(channel, "bookTicker") || strings.Contains(channel, channelTicker) {
			return channelTicker
		}
	}
	return ""
}

// GetChannelExtractor extracts routing information for incoming spot messages.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(message []byte) string {
		if ch := extractProtoChannel(message); ch != "" {
			return ch
		}
		return extractJSONChannel(message)
	}
}

// ParseTicker parses a spot bookTicker message.
func (a *WsAdapter) ParseTicker(message []byte) (string, *store.PriceData, error) {
	// 1. Try Protobuf decoding
	var wrapper mexcproto.PushDataV3ApiWrapper
	if err := proto.Unmarshal(message, &wrapper); err == nil && wrapper.GetPublicBookTicker() != nil {
		ticker := wrapper.GetPublicBookTicker()
		symbol := wrapper.GetSymbol()
		if symbol == "" {
			symbol = extractSymbolFromChannel(wrapper.GetChannel())
		}
		bid1 := decmath.ParseFloat(ticker.GetBidPrice())
		ask1 := decmath.ParseFloat(ticker.GetAskPrice())
		pd := &store.PriceData{
			Symbol:    symbol,
			BestBid:   bid1,
			BestAsk:   ask1,
			UpdatedAt: time.Now(),
		}
		return symbol, pd, nil
	}

	// 2. Fallback to JSON decoding
	dataBytes, dataType, _, err := jsonparser.Get(message, "d")
	if err != nil || dataType != jsonparser.Object {
		return "", nil, fmt.Errorf("invalid spot ticker data format")
	}

	symbol, _ := jsonparser.GetString(dataBytes, "s")
	bid1Str, _ := jsonparser.GetString(dataBytes, "b")
	ask1Str, _ := jsonparser.GetString(dataBytes, "a")

	var bid1, ask1 float64
	_, _ = fmt.Sscanf(bid1Str, "%f", &bid1)
	_, _ = fmt.Sscanf(ask1Str, "%f", &ask1)

	pd := &store.PriceData{
		Symbol:    symbol,
		BestBid:   bid1,
		BestAsk:   ask1,
		UpdatedAt: time.Now(),
	}
	return symbol, pd, nil
}

// ParsePosition returns nil for spot.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, nil
}

func parseObjectLevel(value []byte) (domain.OrderBookEntry, bool) {
	pStr, err := jsonparser.GetString(value, "price")
	if err != nil {
		pStr, _ = jsonparser.GetString(value, "p")
	}
	vStr, err := jsonparser.GetString(value, "quantity")
	if err != nil {
		vStr, _ = jsonparser.GetString(value, "v")
	}
	p := decmath.ParseFloat(pStr)
	v := decmath.ParseFloat(vStr)
	if p > 0 {
		return domain.OrderBookEntry{Price: p, Volume: v}, true
	}
	return domain.OrderBookEntry{}, false
}

func parseArrayLevel(value []byte) (domain.OrderBookEntry, bool) {
	var p, v float64
	idx := 0
	_, _ = jsonparser.ArrayEach(value, func(val []byte, _ jsonparser.ValueType, _ int, _ error) {
		switch idx {
		case 0:
			p = decmath.ParseFloat(string(val))
		case 1:
			v = decmath.ParseFloat(string(val))
		}
		idx++
	})
	if p > 0 {
		return domain.OrderBookEntry{Price: p, Volume: v}, true
	}
	return domain.OrderBookEntry{}, false
}

func parseLevels(dataNode []byte, key string) ([]domain.OrderBookEntry, error) {
	rawLevels, dt, _, err := jsonparser.Get(dataNode, key)
	if err != nil {
		if errors.Is(err, jsonparser.KeyPathNotFoundError) {
			return nil, nil
		}
		return nil, fmt.Errorf("read spot %s: %w", key, err)
	}
	if dt != jsonparser.Array {
		return nil, nil
	}

	var entries []domain.OrderBookEntry
	var parseErr error

	_, _ = jsonparser.ArrayEach(rawLevels, func(value []byte, dataType jsonparser.ValueType, _ int, _ error) {
		if parseErr != nil {
			return
		}
		switch dataType {
		case jsonparser.Object:
			if entry, ok := parseObjectLevel(value); ok {
				entries = append(entries, entry)
			}
		case jsonparser.Array:
			if entry, ok := parseArrayLevel(value); ok {
				entries = append(entries, entry)
			}
		default:
		}
	})

	return entries, parseErr
}

func extractSpotDepthDataNode(message []byte) []byte {
	if dataBytes, dataType, _, err := jsonparser.Get(message, "publicAggreDepths"); err == nil && dataType == jsonparser.Object {
		return dataBytes
	}
	if dataBytes, dataType, _, err := jsonparser.Get(message, "d"); err == nil && dataType == jsonparser.Object {
		return dataBytes
	}
	return message
}

func extractSpotDepthSymbol(message []byte) string {
	if sym, err := jsonparser.GetString(message, "symbol"); err == nil && sym != "" {
		return sym
	}
	if sym, err := jsonparser.GetString(message, "s"); err == nil && sym != "" {
		return sym
	}
	channel, err := jsonparser.GetString(message, "channel")
	if err != nil {
		channel, _ = jsonparser.GetString(message, "c")
	}
	return extractSymbolFromChannel(channel)
}

func extractSymbolFromChannel(channel string) string {
	if idx := strings.LastIndex(channel, "@"); idx != -1 && idx < len(channel)-1 {
		return channel[idx+1:]
	}
	return ""
}

func extractSpotDepthVersions(dataNode []byte) (int64, int64) {
	var fromVersion, toVersion int64
	if v, err := jsonparser.GetInt(dataNode, "fromVersion"); err == nil {
		fromVersion = v
	} else if vStr, err := jsonparser.GetString(dataNode, "fromVersion"); err == nil {
		fromVersion = decmath.ParseInt64(vStr)
	}

	for _, key := range []string{"toVersion", "v", "version"} {
		if v, err := jsonparser.GetInt(dataNode, key); err == nil {
			toVersion = v
			break
		} else if vStr, err := jsonparser.GetString(dataNode, key); err == nil && vStr != "" {
			toVersion = decmath.ParseInt64(vStr)
			break
		}
	}
	return fromVersion, toVersion
}

func buildOrderBookEntries[T any](items []T, getPrice, getQty func(T) string) []domain.OrderBookEntry {
	entries := make([]domain.OrderBookEntry, 0, len(items))
	for _, item := range items {
		p := decmath.ParseFloat(getPrice(item))
		v := decmath.ParseFloat(getQty(item))
		if p > 0 {
			entries = append(entries, domain.OrderBookEntry{Price: p, Volume: v})
		}
	}
	return entries
}

func parseProtoAggreDepth(symbol string, aggre *mexcproto.PublicAggreDepthsV3Api) *domain.OrderBook {
	fromVer := decmath.ParseInt64(aggre.GetFromVersion())
	toVer := decmath.ParseInt64(aggre.GetToVersion())
	bids := buildOrderBookEntries(aggre.GetBids(), (*mexcproto.PublicAggreDepthV3ApiItem).GetPrice, (*mexcproto.PublicAggreDepthV3ApiItem).GetQuantity)
	asks := buildOrderBookEntries(aggre.GetAsks(), (*mexcproto.PublicAggreDepthV3ApiItem).GetPrice, (*mexcproto.PublicAggreDepthV3ApiItem).GetQuantity)

	return &domain.OrderBook{
		Symbol:       symbol,
		FirstVersion: fromVer,
		Version:      toVer,
		Bids:         bids,
		Asks:         asks,
	}
}

func parseProtoIncreaseDepth(symbol string, inc *mexcproto.PublicIncreaseDepthsV3Api) *domain.OrderBook {
	ver := decmath.ParseInt64(inc.GetVersion())
	bids := buildOrderBookEntries(inc.GetBids(), (*mexcproto.PublicIncreaseDepthV3ApiItem).GetPrice, (*mexcproto.PublicIncreaseDepthV3ApiItem).GetQuantity)
	asks := buildOrderBookEntries(inc.GetAsks(), (*mexcproto.PublicIncreaseDepthV3ApiItem).GetPrice, (*mexcproto.PublicIncreaseDepthV3ApiItem).GetQuantity)

	return &domain.OrderBook{
		Symbol:  symbol,
		Version: ver,
		Bids:    bids,
		Asks:    asks,
	}
}

func parseProtoLimitDepth(symbol string, lim *mexcproto.PublicLimitDepthsV3Api) *domain.OrderBook {
	ver := decmath.ParseInt64(lim.GetVersion())
	bids := buildOrderBookEntries(lim.GetBids(), (*mexcproto.PublicLimitDepthV3ApiItem).GetPrice, (*mexcproto.PublicLimitDepthV3ApiItem).GetQuantity)
	asks := buildOrderBookEntries(lim.GetAsks(), (*mexcproto.PublicLimitDepthV3ApiItem).GetPrice, (*mexcproto.PublicLimitDepthV3ApiItem).GetQuantity)

	return &domain.OrderBook{
		Symbol:  symbol,
		Version: ver,
		Bids:    bids,
		Asks:    asks,
	}
}

func parseProtoDepth(message []byte) (string, *domain.OrderBook, bool) {
	var wrapper mexcproto.PushDataV3ApiWrapper
	if err := proto.Unmarshal(message, &wrapper); err != nil {
		return "", nil, false
	}
	symbol := wrapper.GetSymbol()
	if symbol == "" {
		symbol = extractSymbolFromChannel(wrapper.GetChannel())
	}
	if aggre := wrapper.GetPublicAggreDepths(); aggre != nil {
		return symbol, parseProtoAggreDepth(symbol, aggre), true
	}
	if inc := wrapper.GetPublicIncreaseDepths(); inc != nil {
		return symbol, parseProtoIncreaseDepth(symbol, inc), true
	}
	if lim := wrapper.GetPublicLimitDepths(); lim != nil {
		return symbol, parseProtoLimitDepth(symbol, lim), true
	}
	return "", nil, false
}

// ParseDepth parses a spot incremental depth update message into domain.OrderBook.
func (a *WsAdapter) ParseDepth(message []byte) (string, *domain.OrderBook, error) {
	if sym, ob, ok := parseProtoDepth(message); ok {
		return sym, ob, nil
	}

	dataBytes := extractSpotDepthDataNode(message)
	symbol := extractSpotDepthSymbol(message)
	fromVersion, toVersion := extractSpotDepthVersions(dataBytes)

	bids, err := parseLevels(dataBytes, "bids")
	if err != nil {
		return symbol, nil, err
	}
	asks, err := parseLevels(dataBytes, "asks")
	if err != nil {
		return symbol, nil, err
	}

	return symbol, &domain.OrderBook{
		Symbol:       symbol,
		FirstVersion: fromVersion,
		Version:      toVersion,
		Bids:         bids,
		Asks:         asks,
	}, nil
}
