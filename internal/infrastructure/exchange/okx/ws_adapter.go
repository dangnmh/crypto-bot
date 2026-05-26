package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"
)

// WsAdapter implements ws.ExchangeAdapter for OKX V5.
type WsAdapter struct {
	pool *pkgws.Pool
}

// NewWsAdapter creates a new OKX WsAdapter.
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
			{fieldChannel: channelTicker, paramInstId: symbol},
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
			{fieldChannel: channelTicker, paramInstId: symbol},
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
			{fieldChannel: channelKline, paramInstId: symbol},
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
			{fieldChannel: channelKline, paramInstId: symbol},
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
			{fieldChannel: channelDepth, paramInstId: symbol},
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
			{fieldChannel: channelDepth, paramInstId: symbol},
		},
	}
	topic := symbol + ":depth"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to OKX private channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	msg := map[string]interface{}{
		"op": opSubscribe,
		fieldArgs: []map[string]string{
			{fieldChannel: channelOrders, paramInstType: instTypeSwap},
			{fieldChannel: channelPositions, paramInstType: instTypeSwap},
		},
	}
	return a.pool.SendPrivate(ctx, msg)
}

// GetPingConfig returns ping message and interval (OKX expects ping string frame).
func (a *WsAdapter) GetPingConfig() (interface{}, time.Duration) {
	return "ping", 20 * time.Second
}

// GetAuthHook returns connection hook for OKX private auth.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	if apiKey == "" {
		return nil
	}
	return func(c *pkgws.Client) {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		sig := SignRequest(apiSecret, ts, "GET", "/users/self/verify", "")

		msg := map[string]interface{}{
			"op": "login",
			"args": []map[string]interface{}{
				{
					"apiKey":     apiKey,
					"passphrase": "default_passphrase", // Overridden by runtime OKX_PASSPHRASE env
					"timestamp":  ts,
					"sign":       sig,
				},
			},
		}
		_ = c.SendJSON(msg)
	}
}

// GetChannelExtractor maps OKX events to channels.
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
	instID, err := jsonparser.GetString(data, "arg", "instId")
	if err != nil {
		return "", nil, err
	}

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	var rawTicker struct {
		Last      string `json:"last"`
		BidPx     string `json:"bidPx"`
		AskPx     string `json:"askPx"`
		VolCcy24h string `json:"volCcy24h"`
	}

	// OKX data is an array of objects
	var dataArr []json.RawMessage
	if err := json.Unmarshal(dataNode, &dataArr); err != nil || len(dataArr) == 0 {
		return "", nil, fmt.Errorf("parse ticker data node: %w", err)
	}

	if err := json.Unmarshal(dataArr[0], &rawTicker); err != nil {
		return "", nil, err
	}

	last, _ := strconv.ParseFloat(rawTicker.Last, 64)
	bid, _ := strconv.ParseFloat(rawTicker.BidPx, 64)
	ask, _ := strconv.ParseFloat(rawTicker.AskPx, 64)
	vol, _ := strconv.ParseFloat(rawTicker.VolCcy24h, 64)

	pd = &store.PriceData{
		Symbol:    instID,
		LastPrice: last,
		BestBid:   bid,
		BestAsk:   ask,
		FairPrice: last,
		Volume24:  vol,
		UpdatedAt: time.Now(),
	}

	return instID, pd, nil
}

// ParseDepth parses books5 feed into domain.OrderBook.
func (a *WsAdapter) ParseDepth(data []byte) (symbol string, ob *domain.OrderBook, err error) {
	instID, err := jsonparser.GetString(data, "arg", "instId")
	if err != nil {
		return "", nil, err
	}

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	var dataArr []struct {
		Asks [][]string `json:"asks"`
		Bids [][]string `json:"bids"`
		Ts   string     `json:"ts"`
	}
	if err := json.Unmarshal(dataNode, &dataArr); err != nil || len(dataArr) == 0 {
		return "", nil, fmt.Errorf("parse depth data: %w", err)
	}

	tsVal, _ := strconv.ParseInt(dataArr[0].Ts, 10, 64)

	ob = &domain.OrderBook{
		Symbol:  instID,
		Version: tsVal,
		Asks:    make([]domain.OrderBookEntry, 0, len(dataArr[0].Asks)),
		Bids:    make([]domain.OrderBookEntry, 0, len(dataArr[0].Bids)),
	}

	asks := dataArr[0].Asks
	for i := range asks {
		ask := asks[i]
		if len(ask) < 2 {
			continue
		}
		p, _ := strconv.ParseFloat(ask[0], 64)
		v, _ := strconv.ParseFloat(ask[1], 64)
		ob.Asks = append(ob.Asks, domain.OrderBookEntry{Price: p, Volume: v})
	}

	bids := dataArr[0].Bids
	for i := range bids {
		bid := bids[i]
		if len(bid) < 2 {
			continue
		}
		p, _ := strconv.ParseFloat(bid[0], 64)
		v, _ := strconv.ParseFloat(bid[1], 64)
		ob.Bids = append(ob.Bids, domain.OrderBookEntry{Price: p, Volume: v})
	}

	return instID, ob, nil
}

// ParseKline parses candle1m feed into domain.Kline.
func (a *WsAdapter) ParseKline(data []byte) (symbol string, k *domain.Kline, err error) {
	instID, err := jsonparser.GetString(data, "arg", "instId")
	if err != nil {
		return "", nil, err
	}

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	var dataArr [][]string
	if err := json.Unmarshal(dataNode, &dataArr); err != nil || len(dataArr) == 0 {
		return "", nil, fmt.Errorf("parse kline data: %w", err)
	}

	row := dataArr[0]
	if len(row) < 6 {
		return "", nil, fmt.Errorf("insufficient kline fields")
	}

	ts, _ := strconv.ParseInt(row[0], 10, 64)
	o, _ := strconv.ParseFloat(row[1], 64)
	h, _ := strconv.ParseFloat(row[2], 64)
	l, _ := strconv.ParseFloat(row[3], 64)
	c, _ := strconv.ParseFloat(row[4], 64)
	v, _ := strconv.ParseFloat(row[5], 64)
	amt, _ := strconv.ParseFloat(row[6], 64)

	k = &domain.Kline{
		Timestamp: ts,
		Open:      o,
		High:      h,
		Low:       l,
		Close:     c,
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

	var dataArr []okxOrder
	if err := json.Unmarshal(dataNode, &dataArr); err != nil || len(dataArr) == 0 {
		return nil, fmt.Errorf("parse order data: %w", err)
	}

	o := dataArr[0]
	px, _ := strconv.ParseFloat(o.Px, 64)
	sz, _ := strconv.ParseFloat(o.Sz, 64)
	avgPx, _ := strconv.ParseFloat(o.AvgPx, 64)
	fillSz, _ := strconv.ParseFloat(o.FillSz, 64)
	cTime, _ := strconv.ParseInt(o.CTime, 10, 64)
	uTime, _ := strconv.ParseInt(o.UTime, 10, 64)

	deal := &exchange.WsOrderDeal{
		Symbol:       o.InstID,
		OrderID:      o.OrdID,
		Price:        px,
		Vol:          sz,
		DealAvgPrice: avgPx,
		DealVol:      fillSz,
		ExternalOID:  o.ClOrdID,
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
	return nil, fmt.Errorf("ParseOrderDeal not implemented on OKX WS")
}

// ParseTrackOrder is a fallback/unused.
func (a *WsAdapter) ParseTrackOrder(data []byte) (*exchange.PersonalTrackOrderUpdate, error) {
	return nil, fmt.Errorf("ParseTrackOrder not implemented on OKX WS")
}

// ParsePosition parses positions feed into PersonalPositionUpdate.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return nil, err
	}

	var dataArr []okxPosition
	if err := json.Unmarshal(dataNode, &dataArr); err != nil || len(dataArr) == 0 {
		return nil, fmt.Errorf("parse position data: %w", err)
	}

	p := dataArr[0]
	posVal, _ := strconv.ParseFloat(p.Pos, 64)
	leverVal, _ := strconv.Atoi(p.Lever)
	avgPx, _ := strconv.ParseFloat(p.AvgPx, 64)
	liqPx, _ := strconv.ParseFloat(p.LiqPx, 64)
	realized, _ := strconv.ParseFloat(p.RealizedPnl, 64)
	margin, _ := strconv.ParseFloat(p.Margin, 64)

	posType := 1 // long
	if p.PosSide == posSideShort {
		posType = 2
	}

	openType := 1 // isolated
	if p.MgnMode == modeCross {
		openType = 2
	}

	update := &exchange.PersonalPositionUpdate{
		Symbol:         p.InstID,
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
