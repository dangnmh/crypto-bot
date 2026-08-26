package spot

import (
	"context"
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
	dealsStream100ms = "spot@public.aggre.deals.v3.api.pb@100ms@"
	bookTickerStream = "spot@public.bookTicker.v3.api.pb@"
	channelDepth     = "depth"
	channelTicker    = "ticker"
	channelTrade     = "trade"
)

var (
	_ exchange.DepthSubscriber = (*WsAdapter)(nil)
	_ exchange.DepthParser     = (*WsAdapter)(nil)
	_ exchange.TradeSubscriber = (*WsAdapter)(nil)
	_ exchange.TradeParser     = (*WsAdapter)(nil)
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

func cleanMexcSpotSymbol(s string) string {
	return strings.ToUpper(strings.ReplaceAll(s, "_", ""))
}

// SubscribeTicker subscribes to spot bookTicker push.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	stream := bookTickerStream + cleanMexcSpotSymbol(symbol)
	msg := map[string]any{
		methodKey: opSubscription,
		paramsKey: []string{stream},
	}
	topic := symbol + ":ticker"
	return a.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from spot bookTicker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	stream := bookTickerStream + cleanMexcSpotSymbol(symbol)
	msg := map[string]any{
		methodKey: opUnsubscription,
		paramsKey: []string{stream},
	}
	topic := symbol + ":ticker"
	return a.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeDepth subscribes to spot aggregated/diff depth updates (100ms).
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol string) error {
	stream := depthStream100ms + cleanMexcSpotSymbol(symbol)
	msg := map[string]any{
		methodKey: opSubscription,
		paramsKey: []string{stream},
	}
	topic := symbol + ":depth"
	return a.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from spot aggregated/diff depth updates.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol string) error {
	stream := depthStream100ms + cleanMexcSpotSymbol(symbol)
	msg := map[string]any{
		methodKey: opUnsubscription,
		paramsKey: []string{stream},
	}
	topic := symbol + ":depth"
	return a.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeTrade subscribes to spot aggregated public trade deals (100ms).
func (a *WsAdapter) SubscribeTrade(ctx context.Context, symbol string) error {
	stream := dealsStream100ms + cleanMexcSpotSymbol(symbol)
	msg := map[string]any{
		methodKey: opSubscription,
		paramsKey: []string{stream},
	}
	topic := symbol + ":trade"
	return a.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTrade unsubscribes from spot aggregated public trade deals.
func (a *WsAdapter) UnsubscribeTrade(ctx context.Context, symbol string) error {
	stream := dealsStream100ms + cleanMexcSpotSymbol(symbol)
	msg := map[string]any{
		methodKey: opUnsubscription,
		paramsKey: []string{stream},
	}
	topic := symbol + ":trade"
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
	if strings.Contains(channel, "deals") || strings.Contains(channel, "deal") || wrapper.GetPublicAggreDeals() != nil || wrapper.GetPublicDeals() != nil {
		return channelTrade
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
		if strings.Contains(channel, "deals") || strings.Contains(channel, "deal") {
			return channelTrade
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

func extractSymbolFromChannel(channel string) string {
	if idx := strings.LastIndex(channel, "@"); idx != -1 && idx < len(channel)-1 {
		return channel[idx+1:]
	}
	return ""
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

func parseProtoAggreDepth(symbol string, aggre *mexcproto.PublicAggreDepthsV3Api, ts time.Time) *domain.OrderBook {
	fromVer := decmath.ParseInt64(aggre.GetFromVersion())
	toVer := decmath.ParseInt64(aggre.GetToVersion())
	bids := buildOrderBookEntries(aggre.GetBids(), (*mexcproto.PublicAggreDepthV3ApiItem).GetPrice, (*mexcproto.PublicAggreDepthV3ApiItem).GetQuantity)
	asks := buildOrderBookEntries(aggre.GetAsks(), (*mexcproto.PublicAggreDepthV3ApiItem).GetPrice, (*mexcproto.PublicAggreDepthV3ApiItem).GetQuantity)

	return &domain.OrderBook{
		Symbol:       symbol,
		FirstVersion: fromVer,
		Version:      toVer,
		Timestamp:    ts,
		Bids:         bids,
		Asks:         asks,
	}
}

func parseProtoIncreaseDepth(symbol string, inc *mexcproto.PublicIncreaseDepthsV3Api, ts time.Time) *domain.OrderBook {
	ver := decmath.ParseInt64(inc.GetVersion())
	bids := buildOrderBookEntries(inc.GetBids(), (*mexcproto.PublicIncreaseDepthV3ApiItem).GetPrice, (*mexcproto.PublicIncreaseDepthV3ApiItem).GetQuantity)
	asks := buildOrderBookEntries(inc.GetAsks(), (*mexcproto.PublicIncreaseDepthV3ApiItem).GetPrice, (*mexcproto.PublicIncreaseDepthV3ApiItem).GetQuantity)

	return &domain.OrderBook{
		Symbol:    symbol,
		Version:   ver,
		Timestamp: ts,
		Bids:      bids,
		Asks:      asks,
	}
}

func parseProtoLimitDepth(symbol string, lim *mexcproto.PublicLimitDepthsV3Api, ts time.Time) *domain.OrderBook {
	ver := decmath.ParseInt64(lim.GetVersion())
	bids := buildOrderBookEntries(lim.GetBids(), (*mexcproto.PublicLimitDepthV3ApiItem).GetPrice, (*mexcproto.PublicLimitDepthV3ApiItem).GetQuantity)
	asks := buildOrderBookEntries(lim.GetAsks(), (*mexcproto.PublicLimitDepthV3ApiItem).GetPrice, (*mexcproto.PublicLimitDepthV3ApiItem).GetQuantity)

	return &domain.OrderBook{
		Symbol:    symbol,
		Version:   ver,
		Timestamp: ts,
		Bids:      bids,
		Asks:      asks,
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

	ts := time.Now().UTC()
	if wrapper.GetSendTime() > 0 {
		ts = time.UnixMilli(wrapper.GetSendTime()).UTC()
	}

	if aggre := wrapper.GetPublicAggreDepths(); aggre != nil {
		return symbol, parseProtoAggreDepth(symbol, aggre, ts), true
	}
	if inc := wrapper.GetPublicIncreaseDepths(); inc != nil {
		return symbol, parseProtoIncreaseDepth(symbol, inc, ts), true
	}
	if lim := wrapper.GetPublicLimitDepths(); lim != nil {
		return symbol, parseProtoLimitDepth(symbol, lim, ts), true
	}
	return "", nil, false
}

// ParseDepth parses a spot incremental depth update message into domain.OrderBook.
func (a *WsAdapter) ParseDepth(message []byte) (string, *domain.OrderBook, error) {
	if sym, ob, ok := parseProtoDepth(message); ok {
		return sym, ob, nil
	}
	return "", nil, fmt.Errorf("failed to parse mexc spot protobuf depth message")
}

func buildProtoTrades[T any](
	symbol string,
	items []T,
	msgTs time.Time,
	getPrice func(T) string,
	getQty func(T) string,
	getTradeType func(T) int32,
	getTime func(T) int64,
) []domain.PublicTrade {
	trades := make([]domain.PublicTrade, 0, len(items))
	for _, item := range items {
		p := decmath.ParseFloat(getPrice(item))
		v := decmath.ParseFloat(getQty(item))
		if p <= 0 || v <= 0 {
			continue
		}
		side := domain.SideOpenLong
		if getTradeType(item) == 2 {
			side = domain.SideOpenShort
		}
		ts := msgTs
		if itemTime := getTime(item); itemTime > 0 {
			ts = time.UnixMilli(itemTime).UTC()
		}
		trades = append(trades, domain.PublicTrade{
			Symbol:    symbol,
			Price:     p,
			Volume:    v,
			Side:      side,
			Timestamp: ts,
		})
	}
	return trades
}

func parseProtoTrade(message []byte) (string, []domain.PublicTrade, bool) {
	var wrapper mexcproto.PushDataV3ApiWrapper
	if err := proto.Unmarshal(message, &wrapper); err != nil {
		return "", nil, false
	}
	symbol := wrapper.GetSymbol()
	if symbol == "" {
		symbol = extractSymbolFromChannel(wrapper.GetChannel())
	}

	msgTs := time.Now().UTC()
	if wrapper.GetSendTime() > 0 {
		msgTs = time.UnixMilli(wrapper.GetSendTime()).UTC()
	}

	if aggre := wrapper.GetPublicAggreDeals(); aggre != nil {
		trades := buildProtoTrades(
			symbol,
			aggre.GetDeals(),
			msgTs,
			(*mexcproto.PublicAggreDealsV3ApiItem).GetPrice,
			(*mexcproto.PublicAggreDealsV3ApiItem).GetQuantity,
			(*mexcproto.PublicAggreDealsV3ApiItem).GetTradeType,
			(*mexcproto.PublicAggreDealsV3ApiItem).GetTime,
		)
		return symbol, trades, true
	}

	if deals := wrapper.GetPublicDeals(); deals != nil {
		trades := buildProtoTrades(
			symbol,
			deals.GetDeals(),
			msgTs,
			(*mexcproto.PublicDealsV3ApiItem).GetPrice,
			(*mexcproto.PublicDealsV3ApiItem).GetQuantity,
			(*mexcproto.PublicDealsV3ApiItem).GetTradeType,
			(*mexcproto.PublicDealsV3ApiItem).GetTime,
		)
		return symbol, trades, true
	}

	return "", nil, false
}

// ParseTrade parses public spot deals stream messages into []domain.PublicTrade.
func (a *WsAdapter) ParseTrade(message []byte) (string, []domain.PublicTrade, error) {
	if sym, trades, ok := parseProtoTrade(message); ok {
		return sym, trades, nil
	}
	return "", nil, fmt.Errorf("failed to parse mexc spot protobuf trade message")
}
