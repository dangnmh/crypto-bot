package mexc

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"
)

// WsAdapter implements ws.ExchangeAdapter for MEXC Futures.
type WsAdapter struct {
	pool *pkgws.Pool
}

// NewWsAdapter creates a new MEXC WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{}
}

// SetPool injects the websocket pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// SubscribeTicker subscribes to ticker push.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		paramMethod: "sub.ticker",
		paramParam:  map[string]string{paramSymbol: symbol},
	}
	topic := symbol + ":" + channelTicker
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from ticker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		paramMethod: "unsub.ticker",
		paramParam:  map[string]string{paramSymbol: symbol},
	}
	topic := symbol + ":" + channelTicker
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeKline subscribes to 1-minute klines.
func (a *WsAdapter) SubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		paramMethod: "sub.kline",
		paramParam:  map[string]string{paramSymbol: symbol, paramInterval: "Min1"},
	}
	topic := symbol + ":" + channelKline
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeKline unsubscribes from klines.
func (a *WsAdapter) UnsubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		paramMethod: "unsub.kline",
		paramParam:  map[string]string{paramSymbol: symbol},
	}
	topic := symbol + ":" + channelKline
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to all private futures channels used by funding flows.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	msg := map[string]interface{}{
		paramMethod: "personal.filter",
		paramParam: map[string]interface{}{
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

// SubscribeDepth subscribes to orderbook depth.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol, step string) error {
	method := "sub.depth.full"
	param := map[string]interface{}{
		paramSymbol: symbol,
		paramLimit:  20,
	}
	if step != "" {
		method = "sub.depth.step"
		param["step"] = step
	}
	msg := map[string]interface{}{
		paramMethod: method,
		paramParam:  param,
	}
	topic := symbol + ":depth:" + step
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from orderbook depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol, step string) error {
	method := "unsub.depth.full"
	param := map[string]interface{}{
		paramSymbol: symbol,
		paramLimit:  20,
	}
	if step != "" {
		method = "unsub.depth.step"
		param["step"] = step
	}
	msg := map[string]interface{}{
		paramMethod: method,
		paramParam:  param,
	}
	topic := symbol + ":depth:" + step
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// GetPingConfig returns the ping payload and interval for MEXC.
func (a *WsAdapter) GetPingConfig() (interface{}, time.Duration) {
	return map[string]string{paramMethod: "ping"}, 15 * time.Second
}

// GetAuthHook returns the OnConnected hook for MEXC authentication.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	if apiKey == "" {
		return nil
	}
	return func(c *pkgws.Client) {
		reqTime := fmt.Sprintf("%d", time.Now().UnixMilli())
		message := apiKey + reqTime
		mac := hmac.New(sha256.New, []byte(apiSecret))
		mac.Write([]byte(message))
		signature := hex.EncodeToString(mac.Sum(nil))

		msg := map[string]interface{}{
			paramMethod: "login",
			paramParam: map[string]interface{}{
				"apiKey":    apiKey,
				"reqTime":   reqTime,
				"signature": signature,
				"subscribe": false,
			},
		}
		_ = c.SendJSON(msg)
	}
}

// GetChannelExtractor returns the function that maps raw MEXC messages to generic internal channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		var baseMsg struct {
			Channel string `json:"channel"`
		}
		if err := json.Unmarshal(data, &baseMsg); err == nil {
			switch baseMsg.Channel {
			case "push.ticker":
				return channelTicker
			case "push.depth.full", "push.depth.step":
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
				if strings.HasPrefix(baseMsg.Channel, "push.") {
					return strings.TrimPrefix(baseMsg.Channel, "push.")
				}
				return baseMsg.Channel
			}
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
	if err = json.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}

	var ticker WsTickerData
	if err = json.Unmarshal(msg.Data, &ticker); err != nil {
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

// ParseDepth parses raw JSON into exchange.OrderBook using jsonparser for speed.
func (a *WsAdapter) ParseDepth(data []byte) (symbol string, ob *exchange.OrderBook, err error) {
	symbolVal, err := jsonparser.GetString(data, paramSymbol)
	if err != nil {
		return "", nil, err
	}
	symbol = symbolVal

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	version, _ := jsonparser.GetInt(dataNode, "version")

	ob = &exchange.OrderBook{
		Symbol:  symbol,
		Version: version,
		Asks:    make([]exchange.OrderBookEntry, 0, 20),
		Bids:    make([]exchange.OrderBookEntry, 0, 20),
	}

	parseLevel := func(value []byte, isAsk bool) {
		var price, vol float64
		idx := 0
		_, _ = jsonparser.ArrayEach(value, func(v []byte, dt jsonparser.ValueType, offset int, err error) {
			switch idx {
			case 0:
				price = parseFloatValue(v, dt)
			case 1:
				vol = parseFloatValue(v, dt)
			}
			idx++
		})

		if price > 0 {
			if isAsk {
				ob.Asks = append(ob.Asks, exchange.OrderBookEntry{Price: price, Volume: vol})
			} else {
				ob.Bids = append(ob.Bids, exchange.OrderBookEntry{Price: price, Volume: vol})
			}
		}
	}

	_, _ = jsonparser.ArrayEach(dataNode, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
		parseLevel(value, true)
	}, "asks")

	_, _ = jsonparser.ArrayEach(dataNode, func(value []byte, dataType jsonparser.ValueType, offset int, err error) {
		parseLevel(value, false)
	}, "bids")

	return symbol, ob, nil
}

// ParseKline parses raw JSON into exchange.Kline.
func (a *WsAdapter) ParseKline(data []byte) (symbol string, k *exchange.Kline, err error) {
	var msg struct {
		Symbol string          `json:"symbol"`
		Data   json.RawMessage `json:"data"`
	}
	if err = json.Unmarshal(data, &msg); err != nil {
		return "", nil, err
	}

	var kData struct {
		A float64 `json:"a"`
		C float64 `json:"c"`
		H float64 `json:"h"`
		L float64 `json:"l"`
		O float64 `json:"o"`
		T int64   `json:"t"`
		V float64 `json:"v"`
	}
	if err = json.Unmarshal(msg.Data, &kData); err != nil {
		return "", nil, err
	}

	kl := &exchange.Kline{
		Timestamp: kData.T * 1000,
		Open:      kData.O,
		Close:     kData.C,
		High:      kData.H,
		Low:       kData.L,
		Volume:    kData.V,
		Amount:    kData.A,
	}

	return msg.Symbol, kl, nil
}

// ParseOrder parses raw JSON into exchange.WsOrderDeal.
func (a *WsAdapter) ParseOrder(data []byte) (*exchange.WsOrderDeal, error) {
	var msg struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	var deal exchange.WsOrderDeal
	if err := json.Unmarshal(msg.Data, &deal); err != nil {
		return nil, err
	}

	return &deal, nil
}

// ParseOrderDeal parses push.personal.order.deal into execution data.
func (a *WsAdapter) ParseOrderDeal(data []byte) (*exchange.PersonalOrderDeal, error) {
	var msg struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	var deal exchange.PersonalOrderDeal
	if err := json.Unmarshal(msg.Data, &deal); err != nil {
		return nil, err
	}

	return &deal, nil
}

// ParseTrackOrder parses push.personal.track.order into trailing order data.
func (a *WsAdapter) ParseTrackOrder(data []byte) (*exchange.PersonalTrackOrderUpdate, error) {
	var msg struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	var update exchange.PersonalTrackOrderUpdate
	if err := json.Unmarshal(msg.Data, &update); err != nil {
		return nil, err
	}

	return &update, nil
}

// ParsePosition parses push.personal.position into position exposure data.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	var update exchange.PersonalPositionUpdate
	if err := json.Unmarshal(msg.Data, &update); err != nil {
		return nil, err
	}

	return &update, nil
}

func parseFloatValue(v []byte, dt jsonparser.ValueType) float64 {
	var val float64
	if dt == jsonparser.String {
		val, _ = strconv.ParseFloat(string(v), 64)
	} else {
		val, _ = jsonparser.ParseFloat(v)
	}
	return val
}
