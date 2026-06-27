package bitmart

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	infraws "crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"
)

const (
	channelPersonalPosition = "personal.position"
	channelTicker           = "ticker"
	channelPong             = "pong"
	channelPing             = "ping"
)

// WsAdapter implements ws.ExchangeAdapter for Bitmart.
type WsAdapter struct {
	pool          *pkgws.Pool
	client        *Client
	privateURL    string
	apiKey        string
	apiSecret     string
	apiPassphrase string
	clock         exchange.Clock
	priceCache    *infraws.PriceCache

	// Auth sync state
	authMu        sync.Mutex
	authenticated chan struct{}
	authErr       error
}

// NewWsAdapter creates a new WsAdapter.
func NewWsAdapter(privateURL, apiPassphrase string) *WsAdapter {
	return &WsAdapter{
		privateURL:    privateURL,
		apiPassphrase: apiPassphrase,
		priceCache:    infraws.NewPriceCache(),
		clock:         exchange.RealClock{},
		authenticated: make(chan struct{}),
	}
}

// SetPool injects the websocket pool.
func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

// SetClient injects the REST client reference.
func (a *WsAdapter) SetClient(client *Client) {
	a.client = client
}

// SetClock configures a custom clock for testing.
func (a *WsAdapter) SetClock(clk exchange.Clock) {
	if clk != nil {
		a.clock = clk
	}
}

// SubscribeTicker streams ticker info.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	topic := fmt.Sprintf("futures/ticker:%s", symbol)
	msg := map[string]any{
		paramAction: actionSubscribe,
		paramArgs:   []string{topic},
	}
	return a.pool.SubscribePublic(ctx, topic, msg)
}

// UnsubscribeTicker stops streaming ticker updates.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	topic := fmt.Sprintf("futures/ticker:%s", symbol)
	msg := map[string]any{
		paramAction: actionUnsubscribe,
		paramArgs:   []string{topic},
	}
	return a.pool.UnsubscribePublic(ctx, topic, msg)
}

// SubscribePersonal subscribes to personal topics (position updates).
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	a.authMu.Lock()
	authCh := a.authenticated
	a.authMu.Unlock()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-authCh:
	}

	a.authMu.Lock()
	err := a.authErr
	a.authMu.Unlock()
	if err != nil {
		return err
	}

	msg := map[string]any{
		paramAction: actionSubscribe,
		paramArgs:   []string{topicPosition},
	}
	return a.pool.SendPrivate(ctx, msg)
}

// GetPingConfig returns application level ping parameters.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return map[string]any{paramAction: actionPing}, 30 * time.Second
}

// GetAuthHook returns the function to authenticate the private WS connection.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	if apiKey == "" {
		a.authMu.Lock()
		a.authErr = nil
		select {
		case <-a.authenticated:
		default:
			close(a.authenticated)
		}
		a.authMu.Unlock()
		return nil
	}
	a.apiKey = apiKey
	a.apiSecret = apiSecret
	return func(c *pkgws.Client) {
		a.authMu.Lock()
		a.authErr = nil
		a.authenticated = make(chan struct{})
		a.authMu.Unlock()

		timestamp := strconv.FormatInt(a.clock.Now().UnixMilli(), 10)
		signature := GenerateSignature(timestamp, a.apiPassphrase, a.apiSecret, "bitmart.WebSocket")
		authMsg := map[string]any{
			paramAction: actionAccess,
			paramArgs: []any{
				a.apiKey,
				timestamp,
				signature,
				"web",
			},
		}
		_ = c.SendJSON(authMsg)
	}
}

// GetChannelExtractor maps WebSocket event keys to handler channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return a.extractChannel
}

func (a *WsAdapter) extractChannel(data []byte) string {
	if bytes.Contains(data, []byte(`"pong"`)) {
		return channelPong
	}
	if bytes.Contains(data, []byte(`"ping"`)) {
		return channelPing
	}

	var msg struct {
		Action  string `json:"action"`
		Group   string `json:"group"`
		Event   string `json:"event"`
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}
	if err := xjson.Unmarshal(data, &msg); err == nil {
		if msg.Action == "subscribe" || msg.Action == "unsubscribe" {
			return ""
		}
		if msg.Action == actionAccess {
			a.authMu.Lock()
			if msg.Success {
				a.authErr = nil
			} else {
				a.authErr = fmt.Errorf("login failed: %s", msg.Error)
				slog.Error("🔴 WebSocket login failed", slog.String("error", msg.Error))
			}
			select {
			case <-a.authenticated:
			default:
				close(a.authenticated)
			}
			a.authMu.Unlock()
			return actionAccess
		}

		target := msg.Group
		if target == "" {
			target = msg.Action
		}

		if strings.HasPrefix(target, "futures/ticker") || strings.HasPrefix(target, "futures/bookticker") {
			return channelTicker
		}
		if strings.HasPrefix(target, "futures/position") || msg.Event == "futures/position" {
			return channelPersonalPosition
		}
	}

	return fallbackChannel(data)
}

func fallbackChannel(data []byte) string {
	str := string(data)
	if strings.Contains(str, `"subscribe"`) || strings.Contains(str, `"unsubscribe"`) {
		return ""
	}
	if strings.Contains(str, "futures/ticker") || strings.Contains(str, "futures/bookticker") {
		return channelTicker
	}
	if strings.Contains(str, "futures/position") {
		return channelPersonalPosition
	}
	return ""
}

type wsTickerDetail struct {
	Symbol       string `json:"symbol"`
	LastPrice    string `json:"last_price"`
	AskPrice     string `json:"ask_price"`
	AskVol       string `json:"ask_vol"`
	BidPrice     string `json:"bid_price"`
	BidVol       string `json:"bid_vol"`
	Volume24h    string `json:"volume_24h"`
	Volume24     string `json:"volume_24"`
	BestBidPrice string `json:"best_bid_price"`
	BestBidVol   string `json:"best_bid_vol"`
	BestAskPrice string `json:"best_ask_price"`
	BestAskVol   string `json:"best_ask_vol"`
}

func unmarshalTickerDetail(data []byte) (wsTickerDetail, error) {
	var msg struct {
		Group string          `json:"group"`
		Data  json.RawMessage `json:"data"`
	}
	if err := xjson.Unmarshal(data, &msg); err != nil {
		return wsTickerDetail{}, err
	}

	var detail wsTickerDetail
	trimmed := bytes.TrimSpace(msg.Data)
	if len(trimmed) == 0 {
		return wsTickerDetail{}, fmt.Errorf("empty ticker data")
	}

	if trimmed[0] == '[' {
		var list []wsTickerDetail
		if err := xjson.Unmarshal(msg.Data, &list); err != nil {
			return wsTickerDetail{}, err
		}
		if len(list) == 0 {
			return wsTickerDetail{}, fmt.Errorf("empty ticker list")
		}
		detail = list[0]
	} else {
		if err := xjson.Unmarshal(msg.Data, &detail); err != nil {
			return wsTickerDetail{}, err
		}
	}

	if detail.Symbol == "" {
		return wsTickerDetail{}, fmt.Errorf("no symbol in ticker update")
	}
	return detail, nil
}

// ParseTicker unmarshals and merges ticker streams.
func (a *WsAdapter) ParseTicker(data []byte) (string, *store.PriceData, error) {
	detail, err := unmarshalTickerDetail(data)
	if err != nil {
		return "", nil, err
	}

	stdSym := detail.Symbol

	var last, bid, ask, vol float64
	if detail.LastPrice != "" {
		last = decmath.ParseFloat(detail.LastPrice)
	}

	bidPrice := detail.BidPrice
	if bidPrice == "" {
		bidPrice = detail.BestBidPrice
	}
	if bidPrice != "" {
		bid = decmath.ParseFloat(bidPrice)
	}

	askPrice := detail.AskPrice
	if askPrice == "" {
		askPrice = detail.BestAskPrice
	}
	if askPrice != "" {
		ask = decmath.ParseFloat(askPrice)
	}

	volStr := detail.Volume24
	if volStr == "" {
		volStr = detail.Volume24h
	}
	if volStr != "" {
		vol = decmath.ParseFloat(volStr)
	}

	a.priceCache.UpdateDepthAndMidPrice(stdSym, bid, ask)
	pd := a.priceCache.UpdateTicker(stdSym, last, 0, vol)
	return stdSym, pd, nil
}

type wsPositionDetail struct {
	Symbol         string `json:"symbol"`
	PositionAmt    string `json:"position_amt"`
	PositionAmount string `json:"position_amount"`
	HoldVolume     string `json:"hold_volume"`
	PositionType   int    `json:"position_type"`
	AvgEntryPrice  string `json:"avg_entry_price"`
	OpenAvgPrice   string `json:"open_avg_price"`
	HoldAvgPrice   string `json:"hold_avg_price"`
	UnrealizedPnL  string `json:"unrealized_pnl"`
	Leverage       string `json:"leverage"`
	OpenType       any    `json:"open_type"`
	PositionSide   string `json:"position_side"`
	CloseVolume    string `json:"close_volume"`
	CloseAvgPrice  string `json:"close_avg_price"`
}

// ParsePosition parses position update pushes.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg struct {
		Group string             `json:"group"`
		Data  []wsPositionDetail `json:"data"`
	}
	if err := xjson.Unmarshal(data, &msg); err != nil {
		var msgDirect struct {
			Group string           `json:"group"`
			Data  wsPositionDetail `json:"data"`
		}
		if errD := xjson.Unmarshal(data, &msgDirect); errD != nil {
			return nil, err
		}
		msg.Group = msgDirect.Group
		msg.Data = []wsPositionDetail{msgDirect.Data}
	}

	if len(msg.Data) == 0 {
		return nil, fmt.Errorf("empty position update data")
	}

	raw := &msg.Data[0]
	vol := decmath.ParseFloat(raw.PositionAmt)
	if vol == 0 {
		vol = decmath.ParseFloat(raw.PositionAmount)
	}
	if vol == 0 {
		vol = decmath.ParseFloat(raw.HoldVolume)
	}

	pType := exchange.PositionTypeLong
	if raw.PositionType == 2 || raw.PositionType == -2 || strings.EqualFold(raw.PositionSide, posSideShort) || raw.PositionSide == "2" {
		pType = exchange.PositionTypeShort
	}

	avgPrice := decmath.ParseFloat(raw.AvgEntryPrice)
	if avgPrice == 0 {
		avgPrice = decmath.ParseFloat(raw.OpenAvgPrice)
	}
	if avgPrice == 0 {
		avgPrice = decmath.ParseFloat(raw.HoldAvgPrice)
	}
	pnl := decmath.ParseFloat(raw.UnrealizedPnL)

	var lev int
	if raw.Leverage != "" {
		if val, err := strconv.ParseFloat(raw.Leverage, 64); err == nil {
			lev = int(val)
		}
	}

	closeVol := decmath.ParseFloat(raw.CloseVolume)
	closeAvgPrice := decmath.ParseFloat(raw.CloseAvgPrice)

	return &exchange.PersonalPositionUpdate{
		Symbol:          raw.Symbol,
		HoldVol:         vol,
		PositionType:    pType,
		HoldAvgPrice:    avgPrice,
		OpenAvgPrice:    avgPrice,
		CloseVol:        closeVol,
		CloseAvgPrice:   closeAvgPrice,
		CloseProfitLoss: pnl,
		Leverage:        lev,
		UpdateTime:      a.clock.Now().UnixMilli(),
	}, nil
}
