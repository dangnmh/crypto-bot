package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"
)

const msgPong = "pong"

// WsAdapter implements ws.ExchangeAdapter for Bitget Futures V2.
type WsAdapter struct {
	pool *pkgws.Pool
}

// NewWsAdapter creates a new Bitget WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{}
}

// SetPool injects the websocket pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// SubscribeTicker subscribes to ticker stream.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		"op": opSubscribe,
		fieldArgs: []map[string]string{
			{fieldInstType: productTypeUsdtFutures, fieldChannel: channelTicker, fieldInstId: symbol},
		},
	}
	topic := symbol + ":tickers"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from ticker stream.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		"op": opUnsubscribe,
		fieldArgs: []map[string]string{
			{fieldInstType: productTypeUsdtFutures, fieldChannel: channelTicker, fieldInstId: symbol},
		},
	}
	topic := symbol + ":tickers"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeKline subscribes to 1-minute klines.
func (a *WsAdapter) SubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		"op": opSubscribe,
		fieldArgs: []map[string]string{
			{fieldInstType: productTypeUsdtFutures, fieldChannel: channelKline, fieldInstId: symbol},
		},
	}
	topic := symbol + ":kline"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeKline unsubscribes from klines.
func (a *WsAdapter) UnsubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		"op": opUnsubscribe,
		fieldArgs: []map[string]string{
			{fieldInstType: productTypeUsdtFutures, fieldChannel: channelKline, fieldInstId: symbol},
		},
	}
	topic := symbol + ":kline"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeDepth subscribes to depth.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol, step string) error {
	msg := map[string]interface{}{
		"op": opSubscribe,
		fieldArgs: []map[string]string{
			{fieldInstType: productTypeUsdtFutures, fieldChannel: channelDepth, fieldInstId: symbol},
		},
	}
	topic := symbol + ":depth"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol, step string) error {
	msg := map[string]interface{}{
		"op": opUnsubscribe,
		fieldArgs: []map[string]string{
			{fieldInstType: productTypeUsdtFutures, fieldChannel: channelDepth, fieldInstId: symbol},
		},
	}
	topic := symbol + ":depth"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to Bitget private channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	msg := map[string]interface{}{
		"op": opSubscribe,
		fieldArgs: []map[string]string{
			{fieldInstType: productTypeUsdtFutures, fieldChannel: channelOrders, fieldInstId: constantDefault},
			{fieldInstType: productTypeUsdtFutures, fieldChannel: channelPositions, fieldInstId: constantDefault},
		},
	}
	return a.pool.SendPrivate(ctx, msg)
}

// GetPingConfig returns ping message and interval (Bitget V2 expects ping string frame).
func (a *WsAdapter) GetPingConfig() (interface{}, time.Duration) {
	return "ping", 30 * time.Second
}

// GetAuthHook returns connection hook for Bitget private auth.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	if apiKey == "" {
		return nil
	}
	return func(c *pkgws.Client) {
		ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
		sig := SignRequest(apiSecret, ts, "GET", "/user/verify", "")

		// Bitget WS login passphrase defaults to empty or standard if env is set,
		// let's grab it from client configuration if available or pass os env.
		passphrase := os.Getenv("BITGET_PASSPHRASE")

		msg := map[string]interface{}{
			"op": opSubscribe,
			fieldArgs: []map[string]interface{}{
				{
					"apiKey":     apiKey,
					"passphrase": passphrase,
					"timestamp":  ts,
					"sign":       sig,
				},
			},
		}
		_ = c.SendJSON(msg)
	}
}

// GetChannelExtractor maps Bitget events to channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		if string(data) == msgPong {
			return msgPong
		}

		channel, err := jsonparser.GetString(data, "arg", "channel")
		if err != nil {
			return ""
		}

		switch channel {
		case channelTicker:
			return "tickers"
		case channelKline:
			return "kline"
		case channelDepth:
			return "depth"
		case channelOrders:
			return "personal.order"
		case channelPositions:
			return "personal.position"
		}

		return channel
	}
}

// ParseTicker parses ticker feed into store.PriceData.
func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	instID, err := jsonparser.GetString(data, "arg", fieldInstId)
	if err != nil {
		return "", nil, err
	}

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	var dataArr []struct {
		Symbol     string `json:"symbol"`
		InstID     string `json:"instId"`
		LastPr     string `json:"lastPr"`
		BidPr      string `json:"bidPr"`
		AskPr      string `json:"askPr"`
		BaseVolume string `json:"baseVolume"`
	}

	if err := json.Unmarshal(dataNode, &dataArr); err != nil || len(dataArr) == 0 {
		return "", nil, fmt.Errorf("parse ticker data node: %w", err)
	}

	rawTicker := &dataArr[0]
	last, _ := strconv.ParseFloat(rawTicker.LastPr, 64)
	bid, _ := strconv.ParseFloat(rawTicker.BidPr, 64)
	ask, _ := strconv.ParseFloat(rawTicker.AskPr, 64)
	vol, _ := strconv.ParseFloat(rawTicker.BaseVolume, 64)

	sym := rawTicker.Symbol
	if sym == "" {
		sym = rawTicker.InstID
	}
	if sym == "" {
		sym = instID
	}

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
	instID, err := jsonparser.GetString(data, "arg", fieldInstId)
	if err != nil {
		return "", nil, err
	}

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	var dataArr []struct {
		Symbol string          `json:"symbol"`
		InstID string          `json:"instId"`
		Asks   [][]interface{} `json:"asks"`
		Bids   [][]interface{} `json:"bids"`
		Ts     interface{}     `json:"ts"`
	}
	if err := json.Unmarshal(dataNode, &dataArr); err != nil || len(dataArr) == 0 {
		return "", nil, fmt.Errorf("parse depth data: %w", err)
	}

	depthItem := &dataArr[0]
	sym := depthItem.Symbol
	if sym == "" {
		sym = depthItem.InstID
	}
	if sym == "" {
		sym = instID
	}

	tsVal := parseInt(depthItem.Ts)

	ob = &domain.OrderBook{
		Symbol:  sym,
		Version: tsVal,
		Asks:    make([]domain.OrderBookEntry, 0, len(depthItem.Asks)),
		Bids:    make([]domain.OrderBookEntry, 0, len(depthItem.Bids)),
	}

	for i := range depthItem.Asks {
		ask := depthItem.Asks[i]
		if len(ask) < 2 {
			continue
		}
		p := parseFloat(ask[0])
		v := parseFloat(ask[1])
		ob.Asks = append(ob.Asks, domain.OrderBookEntry{Price: p, Volume: v})
	}

	for i := range depthItem.Bids {
		bid := depthItem.Bids[i]
		if len(bid) < 2 {
			continue
		}
		p := parseFloat(bid[0])
		v := parseFloat(bid[1])
		ob.Bids = append(ob.Bids, domain.OrderBookEntry{Price: p, Volume: v})
	}

	return sym, ob, nil
}

// ParseKline parses candle1m feed into domain.Kline.
func (a *WsAdapter) ParseKline(data []byte) (symbol string, k *domain.Kline, err error) {
	instID, err := jsonparser.GetString(data, "arg", fieldInstId)
	if err != nil {
		return "", nil, err
	}

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	var dataArr [][]interface{}
	if err := json.Unmarshal(dataNode, &dataArr); err != nil || len(dataArr) == 0 {
		return "", nil, fmt.Errorf("parse kline data: %w", err)
	}

	row := dataArr[0]
	if len(row) < 6 {
		return "", nil, fmt.Errorf("insufficient kline fields")
	}

	ts := parseInt(row[0])
	o := parseFloat(row[1])
	h := parseFloat(row[2])
	l := parseFloat(row[3])
	cVal := parseFloat(row[4])
	v := parseFloat(row[5])
	amt := parseFloat(row[6])

	k = &domain.Kline{
		Timestamp: ts,
		Open:      o,
		High:      h,
		Low:       l,
		Close:     cVal,
		Volume:    v,
		Amount:    amt,
	}

	return instID, k, nil
}

// ParseOrder parses orders feed into WsOrderDeal.
func (a *WsAdapter) ParseOrder(data []byte) (*exchange.WsOrderDeal, error) {
	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return nil, err
	}

	var dataArr []bitgetOrder
	if err := json.Unmarshal(dataNode, &dataArr); err != nil || len(dataArr) == 0 {
		return nil, fmt.Errorf("parse order data: %w", err)
	}

	o := dataArr[0]
	px, _ := strconv.ParseFloat(o.Price, 64)
	sz, _ := strconv.ParseFloat(o.Size, 64)
	avgPx, _ := strconv.ParseFloat(o.PriceAvg, 64)
	fillSz, _ := strconv.ParseFloat(o.BaseVolume, 64)
	cTime := parseInt(o.CTime)
	uTime := parseInt(o.UTime)

	deal := &exchange.WsOrderDeal{
		Symbol:       o.Symbol,
		OrderID:      o.OrderID,
		Price:        px,
		Vol:          sz,
		DealAvgPrice: avgPx,
		DealVol:      fillSz,
		ExternalOID:  o.ClientOid,
		CreateTime:   cTime,
		UpdateTime:   uTime,
		PositionMode: 2, // net by default
	}

	switch o.PosSide {
	case posSideLong:
		deal.PositionMode = 1
		if o.Side == sideBuy {
			deal.Side = exchange.SideOpenLong
		} else {
			deal.Side = exchange.SideCloseLong
		}
	case posSideShort:
		deal.PositionMode = 1
		if o.Side == sideSell {
			deal.Side = exchange.SideOpenShort
		} else {
			deal.Side = exchange.SideCloseShort
		}
	default:
		if o.Side == sideBuy {
			deal.Side = exchange.SideOpenLong
		} else {
			deal.Side = exchange.SideOpenShort
		}
	}

	switch o.State {
	case stateFilled:
		deal.State = exchange.OrderStateFilled
	case stateCanceled:
		deal.State = exchange.OrderStateCanceled
	default:
		deal.State = exchange.OrderStatePartial
	}

	return deal, nil
}

// ParseOrderDeal is a fallback/unused.
func (a *WsAdapter) ParseOrderDeal(data []byte) (*exchange.PersonalOrderDeal, error) {
	return nil, fmt.Errorf("ParseOrderDeal not implemented on Bitget WS")
}

// ParseTrackOrder is a fallback/unused.
func (a *WsAdapter) ParseTrackOrder(data []byte) (*exchange.PersonalTrackOrderUpdate, error) {
	return nil, fmt.Errorf("ParseTrackOrder not implemented on Bitget WS")
}

// ParsePosition parses positions feed into PersonalPositionUpdate.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return nil, err
	}

	var dataArr []bitgetPosition
	if err := json.Unmarshal(dataNode, &dataArr); err != nil || len(dataArr) == 0 {
		return nil, fmt.Errorf("parse position data: %w", err)
	}

	p := &dataArr[0]
	posVal, _ := strconv.ParseFloat(p.Total, 64)
	leverVal, _ := strconv.Atoi(p.Leverage)
	avgPx, _ := strconv.ParseFloat(p.OpenPriceAvg, 64)
	liqPx, _ := strconv.ParseFloat(p.LiquidationPrice, 64)
	realized, _ := strconv.ParseFloat(p.AchievedProfits, 64)
	margin, _ := strconv.ParseFloat(p.MarginSize, 64)

	posType := 1 // long
	if p.HoldSide == posSideShort {
		posType = 2
	}

	openType := 1 // isolated
	switch p.MarginMode {
	case "crossed", modeCross:
		openType = 2
	}

	update := &exchange.PersonalPositionUpdate{
		Symbol:         p.Symbol,
		HoldVol:        posVal,
		Leverage:       leverVal,
		HoldAvgPrice:   avgPx,
		LiquidatePrice: liqPx,
		Realized:       realized,
		IM:             margin,
		PositionType:   posType,
		OpenType:       openType,
		State:          1,
	}

	return update, nil
}
