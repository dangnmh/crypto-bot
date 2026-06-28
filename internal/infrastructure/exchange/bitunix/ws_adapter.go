package bitunix

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
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
)

// WsAdapter implements ws.ExchangeAdapter for Bitunix.
type WsAdapter struct {
	pool          *pkgws.Pool
	client        *Client
	apiKey        string
	apiSecret     string
	clock         exchange.Clock
	priceCache    *infraws.PriceCache
	authMu        sync.Mutex
	authenticated chan struct{}
	authErr       error
}

// NewWsAdapter creates a new WsAdapter.
func NewWsAdapter() *WsAdapter {
	return &WsAdapter{
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

// SubscribeTicker streams ticker info from both "tickers" and "ticker" channels.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	topicTickers := fmt.Sprintf("tickers:%s", strings.ToUpper(symbol))
	msgTickers := map[string]any{
		"op": opSubscribe,
		paramArgs: []any{
			map[string]any{
				paramSymbol: strings.ToUpper(symbol),
				"ch":        channelTickers,
			},
		},
	}
	if err := a.pool.SubscribePublic(ctx, topicTickers, msgTickers); err != nil {
		return err
	}

	topicTicker := fmt.Sprintf("ticker:%s", strings.ToUpper(symbol))
	msgTicker := map[string]any{
		"op": opSubscribe,
		paramArgs: []any{
			map[string]any{
				paramSymbol: strings.ToUpper(symbol),
				"ch":        channelTicker,
			},
		},
	}
	return a.pool.SubscribePublic(ctx, topicTicker, msgTicker)
}

// UnsubscribeTicker stops streaming ticker updates from both channels.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	topicTickers := fmt.Sprintf("tickers:%s", strings.ToUpper(symbol))
	msgTickers := map[string]any{
		"op": opUnsubscribe,
		paramArgs: []any{
			map[string]any{
				paramSymbol: strings.ToUpper(symbol),
				"ch":        channelTickers,
			},
		},
	}
	_ = a.pool.UnsubscribePublic(ctx, topicTickers, msgTickers)

	topicTicker := fmt.Sprintf("ticker:%s", strings.ToUpper(symbol))
	msgTicker := map[string]any{
		"op": opUnsubscribe,
		paramArgs: []any{
			map[string]any{
				paramSymbol: strings.ToUpper(symbol),
				"ch":        channelTicker,
			},
		},
	}
	return a.pool.UnsubscribePublic(ctx, topicTicker, msgTicker)
}

// SubscribePersonal subscribes to position and order update pushes.
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
		"op":      opSubscribe,
		paramArgs: []string{topicPosition, topicOrder},
	}
	return a.pool.SendPrivate(ctx, msg)
}

type bitunixPingMessage struct {
	Op string `json:"op"`
}

func (m bitunixPingMessage) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"op":   m.Op,
		"ping": time.Now().Unix(),
	})
}

// GetPingConfig returns application level ping parameters.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return bitunixPingMessage{Op: opPing}, 30 * time.Second
}

func (a *WsAdapter) generateLoginSignature(nonce string, timestamp int64) string {
	tsStr := strconv.FormatInt(timestamp, 10)
	firstInput := nonce + tsStr + a.apiKey

	firstHash := sha256.Sum256([]byte(firstInput))
	firstDigest := hex.EncodeToString(firstHash[:])

	secondInput := firstDigest + a.apiSecret
	secondHash := sha256.Sum256([]byte(secondInput))
	return hex.EncodeToString(secondHash[:])
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

		nonce := generateNonce()
		timestamp := a.clock.Now().UnixMilli()
		signature := a.generateLoginSignature(nonce, timestamp)

		authMsg := map[string]any{
			"op": opLogin,
			paramArgs: []any{
				map[string]any{
					"apiKey":    a.apiKey,
					"timestamp": timestamp,
					"nonce":     nonce,
					paramSign:   signature,
				},
			},
		}
		_ = c.SendJSON(authMsg)
	}
}

// GetChannelExtractor maps WebSocket event keys to handler channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return a.extractChannel
}

func (a *WsAdapter) handleLoginResponse(data []byte) {
	a.authMu.Lock()
	defer a.authMu.Unlock()

	var authResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	_ = json.Unmarshal(data, &authResp)
	if authResp.Code == 0 || strings.EqualFold(authResp.Msg, "success") {
		a.authErr = nil
	} else {
		a.authErr = fmt.Errorf("login failed code %d: %s", authResp.Code, authResp.Msg)
		slog.Error("🔴 WebSocket login failed", slog.String("error", authResp.Msg))
	}
	select {
	case <-a.authenticated:
	default:
		close(a.authenticated)
	}
}

func (a *WsAdapter) extractChannel(data []byte) string {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return ""
	}

	var msg struct {
		Topic string `json:"topic"`
		Event string `json:"event"`
		Op    string `json:"op"`
		Ch    string `json:"ch"`
	}
	if err := json.Unmarshal(data, &msg); err == nil {
		if msg.Op == opLogin {
			a.handleLoginResponse(data)
			return opLogin
		}

		if msg.Ch == channelTickers || msg.Ch == channelTicker || strings.HasPrefix(msg.Topic, "ticker:") {
			return "ticker"
		}
		if msg.Topic == topicOrder {
			return "personal.order"
		}
		if msg.Ch == "position" || msg.Topic == topicPosition {
			return "personal.position"
		}
		if msg.Op == eventPong || msg.Event == eventPong {
			return eventPong
		}
	}
	return ""
}

// ParseTicker unmarshals ticker data and updates PriceCache.
func (a *WsAdapter) ParseTicker(data []byte) (string, *store.PriceData, error) {
	var envelope struct {
		Ch   string          `json:"ch"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return "", nil, fmt.Errorf("unmarshal ticker envelope: %w", err)
	}

	type tickerItem struct {
		S  string `json:"s"`  // symbol
		La string `json:"la"` // last price
		B  string `json:"b"`  // volume
		Bd string `json:"bd"` // best bid price
		Ak string `json:"ak"` // best ask price
	}

	raw := bytes.TrimSpace(envelope.Data)
	if len(raw) == 0 {
		return "", nil, fmt.Errorf("empty ticker data")
	}

	var item tickerItem
	if raw[0] == '[' {
		var list []tickerItem
		if err := json.Unmarshal(raw, &list); err != nil {
			return "", nil, fmt.Errorf("unmarshal tickers list: %w", err)
		}
		if len(list) == 0 {
			return "", nil, fmt.Errorf("empty tickers list")
		}
		item = list[0]
	} else {
		if err := json.Unmarshal(raw, &item); err != nil {
			return "", nil, fmt.Errorf("unmarshal ticker item: %w", err)
		}
	}

	if item.S == "" {
		return "", nil, fmt.Errorf("missing symbol in ticker update")
	}

	price := decmath.ParseFloat(item.La)
	vol := decmath.ParseFloat(item.B)

	pd := a.priceCache.UpdateTicker(item.S, price, price, vol)

	if envelope.Ch == channelTickers {
		bid := decmath.ParseFloat(item.Bd)
		ask := decmath.ParseFloat(item.Ak)
		pd = a.priceCache.UpdateDepth(item.S, bid, ask)
	}

	return item.S, pd, nil
}

type flexTime int64

func (ft *flexTime) UnmarshalJSON(b []byte) error {
	if len(b) == 0 {
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			t, err = time.Parse("2006-01-02 15:04:05", s)
		}
		if err == nil {
			*ft = flexTime(t.UnixMilli())
			return nil
		}
		if val, err := strconv.ParseInt(s, 10, 64); err == nil {
			*ft = flexTime(val)
			return nil
		}
		if val, err := strconv.ParseFloat(s, 64); err == nil {
			*ft = flexTime(int64(val))
			return nil
		}
	}
	var i int64
	if err := json.Unmarshal(b, &i); err == nil {
		*ft = flexTime(i)
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		*ft = flexTime(int64(f))
		return nil
	}
	return fmt.Errorf("cannot unmarshal %s into flexTime", string(b))
}

type wsPositionData struct {
	Symbol           string   `json:"symbol"`
	Size             string   `json:"size"`
	Qty              string   `json:"qty"`
	EntryPrice       string   `json:"entryPrice"`
	Side             string   `json:"side"` // "LONG", "SHORT"
	Leverage         string   `json:"leverage"`
	UnrealizedProfit string   `json:"unrealizedProfit"`
	UnrealizedPNL    string   `json:"unrealizedPNL"`
	UpdateTime       int64    `json:"updateTime"`
	CTime            flexTime `json:"ctime"`
	Event            string   `json:"event"`
}

// ParsePosition parses position updates.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var envelope struct {
		Topic string           `json:"topic"`
		Ch    string           `json:"ch"`
		Data  []wsPositionData `json:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		var envelopeSingle struct {
			Topic string         `json:"topic"`
			Ch    string         `json:"ch"`
			Data  wsPositionData `json:"data"`
		}
		if errSingle := json.Unmarshal(data, &envelopeSingle); errSingle != nil {
			return nil, fmt.Errorf("unmarshal position envelope: %w", errSingle)
		}
		envelope.Topic = envelopeSingle.Topic
		envelope.Ch = envelopeSingle.Ch
		envelope.Data = []wsPositionData{envelopeSingle.Data}
	}

	if len(envelope.Data) == 0 {
		return nil, fmt.Errorf("no position data in update")
	}

	raw := envelope.Data[0]
	if raw.Symbol == "" {
		return nil, fmt.Errorf("no symbol in position update")
	}

	vol := decmath.ParseFloat(raw.Qty)
	if vol == 0 {
		vol = decmath.ParseFloat(raw.Size)
	}
	if strings.EqualFold(raw.Event, "CLOSE") {
		vol = 0
	}
	pType := exchange.PositionTypeLong
	if strings.EqualFold(raw.Side, "SHORT") {
		pType = exchange.PositionTypeShort
	}

	avgPrice := decmath.ParseFloat(raw.EntryPrice)
	pnl := decmath.ParseFloat(raw.UnrealizedPNL)
	if pnl == 0 {
		pnl = decmath.ParseFloat(raw.UnrealizedProfit)
	}

	var lev int
	if raw.Leverage != "" {
		if val, err := strconv.ParseFloat(raw.Leverage, 64); err == nil {
			lev = int(val)
		}
	}

	utime := raw.UpdateTime
	if utime == 0 {
		utime = int64(raw.CTime)
	}

	return &exchange.PersonalPositionUpdate{
		Symbol:          raw.Symbol,
		HoldVol:         vol,
		PositionType:    pType,
		HoldAvgPrice:    avgPrice,
		OpenAvgPrice:    avgPrice,
		CloseProfitLoss: pnl,
		Leverage:        lev,
		UpdateTime:      utime,
	}, nil
}
