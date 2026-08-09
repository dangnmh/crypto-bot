package aster

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	infraws "crypto-bot/internal/infrastructure/ws"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"

	"github.com/gorilla/websocket"
)

type WsAdapter struct {
	pool         *pkgws.Pool
	client       *Client
	apiKey       string
	apiSecret    string
	passphrase   string
	clock        exchange.Clock
	priceCache   *infraws.PriceCache
	privateURL   string
	cancelKeep   context.CancelFunc
	cancelKeepMu sync.Mutex
}

func NewWsAdapter(apiKey, apiSecret, passphrase, privateURL string) *WsAdapter {
	return &WsAdapter{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		passphrase: passphrase,
		privateURL: privateURL,
		priceCache: infraws.NewPriceCache(),
		clock:      exchange.RealClock{},
	}
}

func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

func (a *WsAdapter) SetClient(client *Client) {
	a.client = client
}

func (a *WsAdapter) SetClock(clk exchange.Clock) {
	if clk != nil {
		a.clock = clk
	}
}

func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	sym := strings.ToLower(symbol)
	tickerMsg := map[string]any{
		paramMethod: "SUBSCRIBE",
		paramParams: []string{sym + "@ticker", sym + "@bookTicker"},
		"id":        1,
	}
	return a.pool.SubscribePublic(ctx, sym+"@ticker", tickerMsg)
}

func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	sym := strings.ToLower(symbol)
	tickerMsg := map[string]any{
		paramMethod: "UNSUBSCRIBE",
		paramParams: []string{sym + "@ticker", sym + "@bookTicker"},
		"id":        1,
	}
	return a.pool.UnsubscribePublic(ctx, sym+"@ticker", tickerMsg)
}

func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	return nil
}

// GetPingConfig returns application ping and interval.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return nil, 0
}

func (a *WsAdapter) GetCustomPingHandler() func(*websocket.Conn, []byte) bool {
	return func(conn *websocket.Conn, data []byte) bool {
		if strings.EqualFold(string(data), "ping") {
			_ = conn.WriteMessage(websocket.TextMessage, []byte("pong"))
			return true
		}
		return false
	}
}

func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	if apiKey != "" {
		a.apiKey = apiKey
	}
	if apiSecret != "" {
		a.apiSecret = apiSecret
	}
	return nil
}

func (a *WsAdapter) HandshakeHeaders() (http.Header, error) {
	headers := http.Header{}
	return headers, nil
}

func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		var msg struct {
			Event     string `json:"e"`
			EventTime int64  `json:"E"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			return ""
		}
		if msg.Event == eventBookTicker || msg.Event == event24hrTicker {
			return "ticker"
		}
		if msg.Event == eventOrderTradeUpdate {
			return "personal.order"
		}
		if msg.Event == eventAccountUpdate {
			return channelPersonalPosition
		}
		return ""
	}
}

type wsBookTicker struct {
	Symbol   string       `json:"s"`
	BidPrice xjson.Number `json:"b"`
	BidQty   xjson.Number `json:"B"`
	AskPrice xjson.Number `json:"a"`
	AskQty   xjson.Number `json:"A"`
	Time     int64        `json:"T"`
}

type ws24hrTicker struct {
	Event     string       `json:"e"`
	Symbol    string       `json:"s"`
	LastPrice xjson.Number `json:"c"`
	Volume    xjson.Number `json:"v"`
	Time      int64        `json:"E"`
}

func (a *WsAdapter) ParseTicker(data []byte) (string, *store.PriceData, error) {
	var base struct {
		Event string `json:"e"`
		Time  int64  `json:"E"`
	}
	if err := json.Unmarshal(data, &base); err != nil {
		return "", nil, err
	}

	if base.Event == eventBookTicker {
		var book wsBookTicker
		if err := json.Unmarshal(data, &book); err != nil {
			return "", nil, err
		}
		symbol := strings.ToUpper(book.Symbol)
		bid, _ := book.BidPrice.Float64()
		ask, _ := book.AskPrice.Float64()

		pd := a.priceCache.UpdateDepth(symbol, bid, ask)
		return symbol, pd, nil
	}

	if base.Event == event24hrTicker {
		var stat ws24hrTicker
		if err := json.Unmarshal(data, &stat); err != nil {
			return "", nil, err
		}
		symbol := strings.ToUpper(stat.Symbol)
		last, _ := stat.LastPrice.Float64()
		vol, _ := stat.Volume.Float64()

		pd := a.priceCache.UpdateTicker(symbol, last, 0, vol)
		return symbol, pd, nil
	}

	return "", nil, fmt.Errorf("unknown event: %s", base.Event)
}

type asterWsAccountUpdate struct {
	Event     string `json:"e"`
	EventTime int64  `json:"E"`
	Update    struct {
		Positions []struct {
			Symbol        string `json:"s"`
			PositionAmt   string `json:"pa"`
			EntryPrice    string `json:"ep"`
			Side          string `json:"ps"`
			UnrealizedPnL string `json:"up"`
		} `json:"P"`
	} `json:"a"`
}

func mapPositionTypeAndAmt(side string, amt float64) (exchange.PositionType, float64) {
	switch {
	case strings.EqualFold(side, "LONG"):
		if amt < 0 {
			return exchange.PositionTypeLong, -amt
		}
		return exchange.PositionTypeLong, amt
	case strings.EqualFold(side, "SHORT"):
		if amt < 0 {
			return exchange.PositionTypeShort, -amt
		}
		return exchange.PositionTypeShort, amt
	default:
		switch {
		case amt < 0:
			return exchange.PositionTypeShort, -amt
		case amt > 0:
			return exchange.PositionTypeLong, amt
		default:
			return exchange.PositionTypeUnknown, 0
		}
	}
}

func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg asterWsAccountUpdate
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	if len(msg.Update.Positions) == 0 {
		return nil, nil
	}

	pos := msg.Update.Positions[0]
	amtRaw, _ := strconv.ParseFloat(pos.PositionAmt, 64)
	entry, _ := strconv.ParseFloat(pos.EntryPrice, 64)
	upVal, _ := strconv.ParseFloat(pos.UnrealizedPnL, 64)

	pType, amt := mapPositionTypeAndAmt(pos.Side, amtRaw)

	return &exchange.PersonalPositionUpdate{
		Symbol:          strings.ToUpper(pos.Symbol),
		HoldVolContract: amt,
		PositionType:    pType,
		OpenAvgPrice:    entry,
		HoldAvgPrice:    entry,
		CloseProfitLoss: upVal,
		UpdateTime:      msg.EventTime,
	}, nil
}

type asterWsOrderUpdate struct {
	Order struct {
		Symbol        string `json:"s"`
		ClientOrderID string `json:"c"`
		Side          string `json:"S"`
		Type          string `json:"o"`
		TimeInForce   string `json:"f"`
		Quantity      string `json:"q"`
		Price         string `json:"p"`
		AvgPrice      string `json:"ap"`
		Status        string `json:"X"`
		OrderID       int64  `json:"i"`
		LastFilledQty string `json:"l"`
		CumFilledQty  string `json:"z"`
		LastPrice     string `json:"L"`
		PositionSide  string `json:"ps"`
		UpdateTime    int64  `json:"T"`
	} `json:"o"`
}

func (a *WsAdapter) ParseOrder(data []byte) (*exchange.WsOrderDeal, error) {
	var msg asterWsOrderUpdate
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	price, _ := strconv.ParseFloat(msg.Order.Price, 64)
	vol, _ := strconv.ParseFloat(msg.Order.Quantity, 64)
	filled, _ := strconv.ParseFloat(msg.Order.CumFilledQty, 64)
	avgPrice, _ := strconv.ParseFloat(msg.Order.AvgPrice, 64)

	var side domain.Side
	isBuy := strings.EqualFold(msg.Order.Side, "BUY")
	isLong := strings.EqualFold(msg.Order.PositionSide, "LONG")
	if isBuy {
		if isLong {
			side = domain.SideOpenLong
		} else {
			side = domain.SideCloseShort
		}
	} else {
		if isLong {
			side = domain.SideCloseLong
		} else {
			side = domain.SideOpenShort
		}
	}

	state := domain.OrderStateNew
	switch msg.Order.Status {
	case statusFilled:
		state = domain.OrderStateFilled
	case statusPartiallyFilled:
		state = domain.OrderStatePartiallyFilled
	case statusCanceled:
		state = domain.OrderStateCanceled
	}

	return &exchange.WsOrderDeal{
		OrderID:      xjson.Number(strconv.FormatInt(msg.Order.OrderID, 10)),
		ExternalOID:  msg.Order.ClientOrderID,
		Symbol:       strings.ToUpper(msg.Order.Symbol),
		Side:         side,
		Price:        price,
		Vol:          vol,
		DealAvgPrice: avgPrice,
		DealVol:      filled,
		State:        state,
		UpdateTime:   msg.Order.UpdateTime,
	}, nil
}

func (a *WsAdapter) GetPrivateURLFunc(ctx context.Context) func() (string, error) {
	return func() (string, error) {
		if a.client == nil {
			return "", fmt.Errorf("aster client not injected in WsAdapter")
		}

		// 1. Fetch listenKey
		listenKey, err := a.client.CreateListenKey(ctx)
		if err != nil {
			return "", fmt.Errorf("create aster listen key failed: %w", err)
		}

		// 2. Prevent goroutine leaks
		a.cancelKeepMu.Lock()
		if a.cancelKeep != nil {
			a.cancelKeep()
		}
		keepCtx, cancel := context.WithCancel(ctx)
		a.cancelKeep = cancel
		a.cancelKeepMu.Unlock()

		// 3. Start keepalive loop
		go a.keepAliveLoop(keepCtx, listenKey)

		// 4. Construct URL
		base := a.privateURL
		if base == "" {
			base = "wss://fstream.asterdex.com/ws"
		}
		base = strings.TrimSuffix(base, "/")
		if !strings.HasSuffix(base, "/ws") {
			base += "/ws"
		}

		return base + "/" + listenKey, nil
	}
}

func (a *WsAdapter) keepAliveLoop(ctx context.Context, listenKey string) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := a.client.KeepAliveListenKey(ctx, listenKey); err != nil {
				a.client.logger.WarnContext(ctx, "failed to keep alive listen key", "error", err)
			}
		}
	}
}
