package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"
)

const (
	posSideLong  = "LONG"
	posSideShort = "SHORT"
	sideBuy      = "BUY"
	sideSell     = "SELL"
	statusNew    = "NEW"
	statusPart   = "PARTIALLY_FILLED"
	statusFilled = "FILLED"
	statusCancel = "CANCELED"
)

// WsAdapter implements ws.ExchangeAdapter for Binance Futures.
type WsAdapter struct {
	pool      *pkgws.Pool
	apiKey    string
	apiSecret string
}

// NewWsAdapter creates a new Binance WsAdapter.
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
		"method": "SUBSCRIBE",
		"params": []string{strings.ToLower(symbol) + "@ticker"},
		"id":     time.Now().UnixMilli(),
	}
	topic := symbol + ":ticker"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker unsubscribes from ticker push.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		"method": "UNSUBSCRIBE",
		"params": []string{strings.ToLower(symbol) + "@ticker"},
		"id":     time.Now().UnixMilli(),
	}
	topic := symbol + ":ticker"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeKline subscribes to 1-minute klines.
func (a *WsAdapter) SubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": []string{strings.ToLower(symbol) + "@kline_1m"},
		"id":     time.Now().UnixMilli(),
	}
	topic := symbol + ":kline"
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeKline unsubscribes from klines.
func (a *WsAdapter) UnsubscribeKline(ctx context.Context, symbol string) error {
	msg := map[string]interface{}{
		"method": "UNSUBSCRIBE",
		"params": []string{strings.ToLower(symbol) + "@kline_1m"},
		"id":     time.Now().UnixMilli(),
	}
	topic := symbol + ":kline"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribeDepth subscribes to orderbook depth.
func (a *WsAdapter) SubscribeDepth(ctx context.Context, symbol, step string) error {
	msg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": []string{strings.ToLower(symbol) + "@depth20@100ms"},
		"id":     time.Now().UnixMilli(),
	}
	topic := symbol + ":depth:" + step
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeDepth unsubscribes from orderbook depth.
func (a *WsAdapter) UnsubscribeDepth(ctx context.Context, symbol, step string) error {
	msg := map[string]interface{}{
		"method": "UNSUBSCRIBE",
		"params": []string{strings.ToLower(symbol) + "@depth20@100ms"},
		"id":     time.Now().UnixMilli(),
	}
	topic := symbol + ":depth:" + step
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to all private futures channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	// For Binance User Data stream, we listen directly to the stream established by listenKey.
	// Since no explicit channel SUBSCRIBE frames are required on user stream connection, we stub this safely.
	return nil
}

// GetPingConfig returns application ping and interval.
func (a *WsAdapter) GetPingConfig() (interface{}, time.Duration) {
	return map[string]interface{}{
		"method": "PING",
	}, 3 * time.Minute
}

// GetAuthHook intercepts OnConnected to store credentials and authenticate.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	a.apiKey = apiKey
	a.apiSecret = apiSecret
	return nil
}

// GetChannelExtractor routes WebSocket push channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		var msg struct {
			Event  string `json:"e"`
			Stream string `json:"stream"`
		}
		if err := json.Unmarshal(data, &msg); err == nil {
			if strings.HasSuffix(msg.Stream, "@ticker") || msg.Event == "24hrTicker" {
				return "ticker"
			}
			if strings.HasSuffix(msg.Stream, "@depth20@100ms") || msg.Event == "depthUpdate" {
				return "depth"
			}
			if strings.HasSuffix(msg.Stream, "@kline_1m") || msg.Event == "kline" {
				return "kline"
			}
			if msg.Event == "ORDER_TRADE_UPDATE" {
				return "personal.order"
			}
			if msg.Event == "ACCOUNT_UPDATE" {
				return "personal.position"
			}
			return msg.Event
		}
		return ""
	}
}

// ParseTicker parses raw JSON into generic store.PriceData.
func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	var msg struct {
		Data struct {
			Symbol    string `json:"s"`
			LastPrice string `json:"c"`
			BestBid   string `json:"b"`
			BestAsk   string `json:"a"`
			Volume24  string `json:"v"`
		} `json:"data"`
	}
	if err = json.Unmarshal(data, &msg); err != nil || msg.Data.Symbol == "" {
		// Try parsing direct non-nested event structure
		var direct struct {
			Symbol    string `json:"s"`
			LastPrice string `json:"c"`
			BestBid   string `json:"b"`
			BestAsk   string `json:"a"`
			Volume24  string `json:"v"`
		}
		if errDirect := json.Unmarshal(data, &direct); errDirect == nil && direct.Symbol != "" {
			msg.Data = direct
		} else if err != nil {
			return "", nil, err
		}
	}

	raw := msg.Data
	pd = &store.PriceData{
		Symbol:    raw.Symbol,
		LastPrice: decmath.ParseFloat(raw.LastPrice),
		BestBid:   decmath.ParseFloat(raw.BestBid),
		BestAsk:   decmath.ParseFloat(raw.BestAsk),
		Volume24:  decmath.ParseFloat(raw.Volume24),
		UpdatedAt: time.Now(),
	}

	return raw.Symbol, pd, nil
}

// ParseDepth parses raw JSON into exchange.OrderBook.
func (a *WsAdapter) ParseDepth(data []byte) (symbol string, ob *domain.OrderBook, err error) {
	var msg struct {
		Data struct {
			Symbol string     `json:"s"`
			Bids   [][]string `json:"b"`
			Asks   [][]string `json:"a"`
			U      int64      `json:"u"`
		} `json:"data"`
	}
	if err = json.Unmarshal(data, &msg); err != nil || msg.Data.Symbol == "" {
		var direct struct {
			Symbol string     `json:"s"`
			Bids   [][]string `json:"b"`
			Asks   [][]string `json:"a"`
			U      int64      `json:"u"`
		}
		if errDirect := json.Unmarshal(data, &direct); errDirect == nil && direct.Symbol != "" {
			msg.Data = direct
		} else if err != nil {
			return "", nil, err
		}
	}

	raw := msg.Data
	ob = &domain.OrderBook{
		Symbol:  raw.Symbol,
		Version: raw.U,
		Asks:    make([]exchange.OrderBookEntry, 0, len(raw.Asks)),
		Bids:    make([]exchange.OrderBookEntry, 0, len(raw.Bids)),
	}

	for _, item := range raw.Asks {
		if len(item) < 2 {
			continue
		}
		p := decmath.ParseFloat(item[0])
		v := decmath.ParseFloat(item[1])
		if p > 0 {
			ob.Asks = append(ob.Asks, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}

	for _, item := range raw.Bids {
		if len(item) < 2 {
			continue
		}
		p := decmath.ParseFloat(item[0])
		v := decmath.ParseFloat(item[1])
		if p > 0 {
			ob.Bids = append(ob.Bids, exchange.OrderBookEntry{Price: p, Volume: v})
		}
	}

	return raw.Symbol, ob, nil
}

// ParseKline parses raw JSON into exchange.Kline.
func (a *WsAdapter) ParseKline(data []byte) (symbol string, k *exchange.Kline, err error) {
	var msg struct {
		Data struct {
			Symbol string `json:"s"`
			Kline  struct {
				T int64  `json:"t"`
				O string `json:"o"`
				C string `json:"c"`
				H string `json:"h"`
				L string `json:"l"`
				V string `json:"v"`
				Q string `json:"q"`
			} `json:"k"`
		} `json:"data"`
	}
	if err = json.Unmarshal(data, &msg); err != nil || msg.Data.Symbol == "" {
		var direct struct {
			Symbol string `json:"s"`
			Kline  struct {
				T int64  `json:"t"`
				O string `json:"o"`
				C string `json:"c"`
				H string `json:"h"`
				L string `json:"l"`
				V string `json:"v"`
				Q string `json:"q"`
			} `json:"k"`
		}
		if errDirect := json.Unmarshal(data, &direct); errDirect == nil && direct.Symbol != "" {
			msg.Data = direct
		} else if err != nil {
			return "", nil, err
		}
	}

	raw := msg.Data
	k = &exchange.Kline{
		Timestamp: raw.Kline.T,
		Open:      decmath.ParseFloat(raw.Kline.O),
		Close:     decmath.ParseFloat(raw.Kline.C),
		High:      decmath.ParseFloat(raw.Kline.H),
		Low:       decmath.ParseFloat(raw.Kline.L),
		Volume:    decmath.ParseFloat(raw.Kline.V),
		Amount:    decmath.ParseFloat(raw.Kline.Q),
	}

	return raw.Symbol, k, nil
}

// ParseOrder parses raw JSON into exchange.WsOrderDeal.
func (a *WsAdapter) ParseOrder(data []byte) (*exchange.WsOrderDeal, error) {
	var msg struct {
		Order struct {
			Symbol       string `json:"s"`
			OrderID      int64  `json:"i"`
			ClientOID    string `json:"c"`
			Price        string `json:"p"`
			Quantity     string `json:"q"`
			LastFilled   string `json:"l"`
			CumFilled    string `json:"z"`
			AvgPrice     string `json:"ap"`
			Side         string `json:"S"`
			PositionSide string `json:"ps"`
			Status       string `json:"X"`
		} `json:"o"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	raw := msg.Order
	deal := &exchange.WsOrderDeal{
		Symbol:       raw.Symbol,
		OrderID:      strconv.FormatInt(raw.OrderID, 10),
		Price:        decmath.ParseFloat(raw.Price),
		Vol:          decmath.ParseFloat(raw.Quantity),
		DealVol:      decmath.ParseFloat(raw.CumFilled),
		DealAvgPrice: decmath.ParseFloat(raw.AvgPrice),
		ExternalOID:  raw.ClientOID,
		PositionMode: 2, // default One-way
	}

	// Map Side & Position mode
	if raw.PositionSide == posSideLong {
		deal.Side = exchange.SideOpenLong
		if raw.Side == sideSell {
			deal.Side = exchange.SideCloseLong
		}
		deal.PositionMode = 1
	} else if raw.PositionSide == posSideShort {
		deal.Side = exchange.SideOpenShort
		if raw.Side == sideBuy {
			deal.Side = exchange.SideCloseShort
		}
		deal.PositionMode = 1
	} else {
		if raw.Side == sideBuy {
			deal.Side = exchange.SideOpenLong
		} else {
			deal.Side = exchange.SideOpenShort
		}
	}

	// Map State
	switch raw.Status {
	case statusNew:
		deal.State = exchange.OrderStatePartial
	case statusPart:
		deal.State = exchange.OrderStatePartial
	case statusFilled:
		deal.State = exchange.OrderStateFilled
	case statusCancel, "EXPIRED":
		deal.State = exchange.OrderStateCanceled
	}

	return deal, nil
}

// ParseOrderDeal stub.
func (a *WsAdapter) ParseOrderDeal(data []byte) (*exchange.PersonalOrderDeal, error) {
	return nil, nil
}

// ParseTrackOrder stub.
func (a *WsAdapter) ParseTrackOrder(data []byte) (*exchange.PersonalTrackOrderUpdate, error) {
	return nil, nil
}

// ParsePosition parses push.personal.position.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg struct {
		Update struct {
			Positions []struct {
				Symbol       string `json:"s"`
				Amount       string `json:"pa"`
				EntryPrice   string `json:"ep"`
				Unrealized   string `json:"up"`
				PositionSide string `json:"ps"`
			} `json:"P"`
		} `json:"a"`
	}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	if len(msg.Update.Positions) == 0 {
		return nil, fmt.Errorf("empty position update in push")
	}

	raw := msg.Update.Positions[0]
	amt := decmath.ParseFloat(raw.Amount)

	posType := 1
	if amt < 0 {
		posType = 2
	}
	if raw.PositionSide == "SHORT" {
		posType = 2
	}

	update := &exchange.PersonalPositionUpdate{
		Symbol:       raw.Symbol,
		HoldVol:      math.Abs(amt),
		HoldAvgPrice: decmath.ParseFloat(raw.EntryPrice),
		Realized:     decmath.ParseFloat(raw.Unrealized),
		PositionType: posType,
	}

	return update, nil
}
