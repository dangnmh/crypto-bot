package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"
)

const msgPong = "pong"

// WsAdapter implements ws.ExchangeAdapter for Bitget Futures V2.
type WsAdapter struct {
	pool          *pkgws.Pool
	authenticated chan struct{}
	authMu        sync.Mutex
}

// NewWsAdapter creates a new Bitget WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{
		authenticated: make(chan struct{}),
	}
}

// SetPool injects the websocket pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// SubscribeTicker subscribes to ticker stream.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
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
	msg := map[string]any{
		"op": opUnsubscribe,
		fieldArgs: []map[string]string{
			{fieldInstType: productTypeUsdtFutures, fieldChannel: channelTicker, fieldInstId: symbol},
		},
	}
	topic := symbol + ":tickers"
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to Bitget private channels.
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
			{fieldInstType: productTypeUsdtFutures, fieldChannel: channelOrders, fieldInstId: constantDefault},
			{fieldInstType: productTypeUsdtFutures, fieldChannel: channelPositions, fieldInstId: constantDefault},
			{fieldInstType: productTypeUsdtFutures, fieldChannel: channelPositionsHistory, fieldInstId: constantDefault},
		},
	}
	return a.pool.SendPrivate(ctx, msg)
}

// GetPingConfig returns ping message and interval (Bitget V2 expects ping string frame).
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return "ping", 30 * time.Second
}

// GetAuthHook returns connection hook for Bitget private auth.
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
	return func(c *pkgws.Client) {
		a.authMu.Lock()
		a.authenticated = make(chan struct{})
		a.authMu.Unlock()

		ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
		sig := SignRequest(apiSecret, ts, "GET", "/user/verify", "")

		// Bitget WS login passphrase defaults to empty or standard if env is set,
		// let's grab it from client configuration if available or pass os env.
		passphrase := os.Getenv("BITGET_PASSPHRASE")

		msg := map[string]any{
			"op": opLogin,
			fieldArgs: []map[string]any{
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

// GetChannelExtractor routes WebSocket push channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		if string(data) == msgPong {
			return msgPong
		}

		if event, err := jsonparser.GetString(data, "event"); err == nil && event == opLogin {
			code, _ := jsonparser.GetString(data, "code")
			if code == "0" || code == "" {
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
			return "tickers"
		case channelKline:
			return "kline"
		case channelDepth:
			return "depth"
		case channelOrders:
			return "personal.order"
		case channelPositions:
			return chPersonalPosition
		case channelPositionsHistory:
			return chPersonalPosition
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

type bitgetHistoryPositionWs struct {
	PosID           string `json:"posId"`
	InstID          string `json:"instId"`
	MarginCoin      string `json:"marginCoin"`
	MarginMode      string `json:"marginMode"`
	HoldSide        string `json:"holdSide"`
	PosMode         string `json:"posMode"`
	OpenPriceAvg    string `json:"openPriceAvg"`
	ClosePriceAvg   string `json:"closePriceAvg"`
	OpenSize        string `json:"openSize"`
	CloseSize       string `json:"closeSize"`
	AchievedProfits string `json:"achievedProfits"`
	SettleFee       string `json:"settleFee"`
	OpenFee         string `json:"openFee"`
	CloseFee        string `json:"closeFee"`
	CTime           string `json:"cTime"`
	UTime           string `json:"uTime"`
}

// ParsePosition parses positions feed into PersonalPositionUpdate.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	channel, _ := jsonparser.GetString(data, "arg", "channel")

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return nil, err
	}

	if channel == channelPositionsHistory {
		var dataArr []bitgetHistoryPositionWs
		if err := json.Unmarshal(dataNode, &dataArr); err != nil || len(dataArr) == 0 {
			return nil, fmt.Errorf("parse history position data: %w", err)
		}

		p := &dataArr[0]
		openPx, _ := strconv.ParseFloat(p.OpenPriceAvg, 64)
		closePx, _ := strconv.ParseFloat(p.ClosePriceAvg, 64)
		closeVol, _ := strconv.ParseFloat(p.CloseSize, 64)
		realized, _ := strconv.ParseFloat(p.AchievedProfits, 64)
		settleFee, _ := strconv.ParseFloat(p.SettleFee, 64)
		openFee, _ := strconv.ParseFloat(p.OpenFee, 64)
		closeFee, _ := strconv.ParseFloat(p.CloseFee, 64)
		uTime := decmath.ParseInt64(p.UTime)

		posType := exchange.PositionTypeLong // long
		if p.HoldSide == posSideShort {
			posType = exchange.PositionTypeShort
		}

		update := &exchange.PersonalPositionUpdate{
			Symbol:          p.InstID,
			HoldVol:         0.0,
			PositionType:    posType,
			OpenAvgPrice:    openPx,
			HoldAvgPrice:    openPx,
			CloseVol:        closeVol,
			CloseAvgPrice:   closePx,
			CloseProfitLoss: realized,
			Fee:             openFee + closeFee,
			HoldFee:         settleFee,
			UpdateTime:      uTime,
		}
		return update, nil
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

	posType := exchange.PositionTypeLong // long
	if p.HoldSide == posSideShort {
		posType = exchange.PositionTypeShort
	}

	sym := p.Symbol
	if sym == "" {
		sym = p.InstID
	}

	update := &exchange.PersonalPositionUpdate{
		Symbol:          sym,
		HoldVol:         posVal,
		Leverage:        leverVal,
		HoldAvgPrice:    avgPx,
		LiquidatePrice:  liqPx,
		CloseProfitLoss: realized,
		PositionType:    posType,
	}

	return update, nil
}
