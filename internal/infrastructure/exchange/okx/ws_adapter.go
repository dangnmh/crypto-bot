package okx

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"

	"crypto-bot/pkg/xjson"
)

// WsAdapter implements ws.ExchangeAdapter for OKX V5.
type WsAdapter struct {
	pool          *pkgws.Pool
	passphrase    string
	authMu        sync.Mutex
	authenticated chan struct{}
	clock         exchange.Clock
}

// NewWsAdapter creates a new OKX WsAdapter.
func NewWsAdapter(passphrase string) *WsAdapter {
	return &WsAdapter{
		passphrase:    passphrase,
		authenticated: make(chan struct{}),
		clock:         exchange.RealClock{},
	}
}

// SetClock configures a custom clock implementation.
func (a *WsAdapter) SetClock(clk exchange.Clock) {
	if clk != nil {
		a.clock = clk
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

// SubscribeTicker subscribes to ticker stream.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
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
	msg := map[string]any{
		"op": opUnsubscribe,
		fieldArgs: []map[string]string{
			{fieldChannel: channelTicker, paramInstId: symbol},
		},
	}
	topic := symbol + ":tickers"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to OKX private channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	a.authMu.Lock()
	authCh := a.authenticated
	a.authMu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-authCh:
	}

	msg := map[string]any{
		"op": opSubscribe,
		fieldArgs: []map[string]string{
			{fieldChannel: channelOrders, paramInstType: instTypeSwap},
			{fieldChannel: channelPositions, paramInstType: instTypeSwap},
		},
	}
	return a.pool.SendPrivate(ctx, msg)
}

func (a *WsAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
}

// GetPingConfig returns ping message and interval (OKX expects ping string frame).
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return "ping", 20 * time.Second
}

// GetAuthHook returns connection hook for OKX private auth.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	if apiKey == "" {
		a.authMu.Lock()
		select {
		case <-a.authenticated:
		default:
			close(a.authenticated)
		}
		a.authMu.Unlock()
		return nil
	}
	passphrase := a.passphrase
	if passphrase == "" {
		passphrase = "default_passphrase"
	}
	return func(c *pkgws.Client) {
		a.authMu.Lock()
		a.authenticated = make(chan struct{})
		a.authMu.Unlock()

		ts := strconv.FormatInt(a.clock.Now().Unix(), 10)
		sig := SignRequest(apiSecret, ts, "GET", "/users/self/verify", "")

		msg := map[string]any{
			"op": opLogin,
			"args": []map[string]any{
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

// GetChannelExtractor maps OKX events to channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		if string(data) == msgPong {
			return msgPong
		}

		if event, err := jsonparser.GetString(data, "event"); err == nil && event == opLogin {
			code, _ := jsonparser.GetString(data, "code")
			if code == "0" {
				a.authMu.Lock()
				select {
				case <-a.authenticated:
				default:
					close(a.authenticated)
				}
				a.authMu.Unlock()
			}
			return opLogin
		}

		channel, err := jsonparser.GetString(data, "arg", "channel")
		if err != nil {
			return ""
		}

		switch channel {
		case channelTicker:
			return "ticker"
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
		if event, _ := jsonparser.GetString(data, "event"); event != "" {
			return "", nil, nil
		}
		return "", nil, err
	}

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		if event, _ := jsonparser.GetString(data, "event"); event != "" {
			return "", nil, nil
		}
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
	if err := xjson.Unmarshal(dataNode, &dataArr); err != nil || len(dataArr) == 0 {
		return "", nil, fmt.Errorf("parse ticker data node: %w", err)
	}

	if err := xjson.Unmarshal(dataArr[0], &rawTicker); err != nil {
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
		UpdatedAt: a.clock.Now(),
	}

	return instID, pd, nil
}

// ParsePosition parses positions feed into PersonalPositionUpdate.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		if event, _ := jsonparser.GetString(data, "event"); event != "" {
			return nil, nil
		}
		return nil, err
	}

	var dataArr []okxPosition
	if err := xjson.Unmarshal(dataNode, &dataArr); err != nil {
		return nil, fmt.Errorf("parse position data: %w", err)
	}
	if len(dataArr) == 0 {
		return nil, nil
	}

	p := dataArr[0]
	posVal, _ := strconv.ParseFloat(p.Pos, 64)
	leverVal, _ := strconv.Atoi(p.Lever)
	avgPx, _ := strconv.ParseFloat(p.AvgPx, 64)
	liqPx, _ := strconv.ParseFloat(p.LiqPx, 64)
	realized, _ := strconv.ParseFloat(p.RealizedPnl, 64)

	posType := mapPositionType(p.PosSide, posVal, p.InstID, p.PosCcy)

	update := &exchange.PersonalPositionUpdate{
		Symbol:          p.InstID,
		HoldVolContract: math.Abs(posVal),
		Leverage:        leverVal,
		HoldAvgPrice:    avgPx,
		LiquidatePrice:  liqPx,
		CloseProfitLoss: realized,
		PositionType:    posType,
	}

	return update, nil
}

// ParseDepth parses depth messages into domain.OrderBook.
func (a *WsAdapter) ParseDepth(data []byte) (string, *domain.OrderBook, error) {
	return "", nil, nil
}
