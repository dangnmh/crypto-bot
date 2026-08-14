package weex

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	infraws "crypto-bot/internal/infrastructure/ws"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"

	"github.com/gorilla/websocket"
)

// WsAdapter implements ws.ExchangeAdapter for WEEX.
type WsAdapter struct {
	pool       *pkgws.Pool
	client     *Client
	apiKey     string
	apiSecret  string
	passphrase string
	clock      exchange.Clock
	priceCache *infraws.PriceCache
}

// NewWsAdapter creates a new WsAdapter.
func NewWsAdapter(apiKey, apiSecret, passphrase string) *WsAdapter {
	return &WsAdapter{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		passphrase: passphrase,
		priceCache: infraws.NewPriceCache(),
		clock:      exchange.RealClock{},
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

const wsPingVal = "ping"

func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	sym := strings.ToUpper(symbol)
	tickerMsg := map[string]any{
		wsKeyMethod: wsMethodSubscribe,
		wsKeyParams: []string{sym + "@ticker"},
		"id":        1,
	}
	if err := a.pool.SubscribePublic(ctx, sym+"@ticker", tickerMsg); err != nil {
		return err
	}

	depthMsg := map[string]any{
		wsKeyMethod: wsMethodSubscribe,
		wsKeyParams: []string{sym + "@depth15"},
		"id":        2,
	}
	return a.pool.SubscribePublic(ctx, sym+"@depth15", depthMsg)
}

func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	sym := strings.ToUpper(symbol)
	tickerMsg := map[string]any{
		wsKeyMethod: wsMethodUnsubscribe,
		wsKeyParams: []string{sym + "@ticker"},
		"id":        1,
	}
	if err := a.pool.UnsubscribePublic(ctx, sym+"@ticker", tickerMsg); err != nil {
		return err
	}

	depthMsg := map[string]any{
		wsKeyMethod: wsMethodUnsubscribe,
		wsKeyParams: []string{sym + "@depth15"},
		"id":        2,
	}
	return a.pool.UnsubscribePublic(ctx, sym+"@depth15", depthMsg)
}

func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	msg := map[string]any{
		wsKeyMethod: wsMethodSubscribe,
		wsKeyParams: []string{wsChannelPositions},
		"id":        7,
	}
	return a.pool.SendPrivate(ctx, msg)
}

func (a *WsAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
}

func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return nil, 0
}

func (a *WsAdapter) GetCustomPingHandler() func(*websocket.Conn, []byte) bool {
	return func(conn *websocket.Conn, data []byte) bool {
		var ping struct {
			Event string `json:"event"`
			Type  string `json:"type"`
		}
		if err := json.Unmarshal(data, &ping); err == nil {
			if strings.EqualFold(ping.Event, wsPingVal) || strings.EqualFold(ping.Type, wsPingVal) {
				pongMsg := map[string]any{
					wsKeyMethod: "PONG",
					"id":        1,
				}
				pongBytes, _ := json.Marshal(pongMsg)
				err := conn.WriteMessage(websocket.TextMessage, pongBytes)
				if err != nil {
					slog.Error("Weex WS: failed to write Pong message", slog.Any("error", err))
				}
				return true
			}
		}
		return false
	}
}

func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	// Save credentials in case they are injected later
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
	headers.Set("User-Agent", "crypto-bot")
	if a.apiKey != "" && a.apiSecret != "" && a.passphrase != "" {
		timestamp := strconv.FormatInt(a.clock.Now().UnixMilli(), 10)
		message := timestamp + "/v3/ws/private"
		mac := hmac.New(sha256.New, []byte(a.apiSecret))
		mac.Write([]byte(message))
		signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		headers.Set("ACCESS-KEY", a.apiKey)
		headers.Set("ACCESS-SIGN", signature)
		headers.Set("ACCESS-TIMESTAMP", timestamp)
		headers.Set("ACCESS-PASSPHRASE", a.passphrase)
	}
	return headers, nil
}

func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		var msg struct {
			Result    *bool  `json:"result"`
			Channel   string `json:"channel"`
			Event     string `json:"event"`
			EventName string `json:"e"`
			EventTime int64  `json:"E"`
			Stream    string `json:"stream"`
			Symbol    string `json:"s"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			return ""
		}
		return a.extractChannelFromMsg(msg.Result, msg.Channel, msg.Event, msg.EventName, msg.Stream, msg.Symbol)
	}
}

func isTickerOrDepth(channel, e, stream, symbol string) bool {
	if stream != "" && (strings.HasSuffix(stream, "@ticker") || strings.HasSuffix(stream, "@depth15")) {
		return true
	}
	if symbol != "" && (e == "ticker" || e == wsChannelDepth) {
		return true
	}
	if channel != "" {
		if strings.HasPrefix(channel, "ticker.") || strings.HasSuffix(channel, "@ticker") || strings.HasSuffix(channel, "@depth15") {
			return true
		}
	}
	return false
}

func (a *WsAdapter) extractChannelFromMsg(result *bool, channel, event, e, stream, symbol string) string {
	if result != nil || event == "subscribed" || event == "SUBSCRIBE" {
		return keySubscribed
	}
	if isTickerOrDepth(channel, e, stream, symbol) {
		return wsChannelTicker
	}
	if stream != "" {
		return strings.ToUpper(stream)
	}
	if channel != "" {
		return channel
	}
	if e != "" {
		if e == wsChannelPositions {
			return "personal.position"
		}
		if e == "orders" {
			return "personal.order"
		}
		return e
	}
	return ""
}

type weexWsTickerItem struct {
	LastPrice xjson.Number `json:"c"`
	MarkPrice xjson.Number `json:"m"`
	Volume24  xjson.Number `json:"v"`
}

func (w *weexWsTickerItem) UnmarshalJSON(data []byte) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	if val, ok := m["c"]; ok {
		_ = json.Unmarshal(val, &w.LastPrice)
	}
	if val, ok := m["m"]; ok {
		_ = json.Unmarshal(val, &w.MarkPrice)
	}
	if val, ok := m["v"]; ok {
		_ = json.Unmarshal(val, &w.Volume24)
	}
	return nil
}

type weexWsTickerPayload struct {
	Symbol string             `json:"s"`
	Data   []weexWsTickerItem `json:"d"`
}

type weexWsDepthPayload struct {
	Symbol string     `json:"s"`
	Bids   [][]string `json:"b"`
	Asks   [][]string `json:"a"`
}

func (a *WsAdapter) parseDepth(data []byte) (string, *store.PriceData, error) {
	var payload weexWsDepthPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", nil, fmt.Errorf("unmarshal depth payload: %w", err)
	}

	sym := strings.ToUpper(payload.Symbol)
	var bid, ask float64
	if len(payload.Bids) > 0 && len(payload.Bids[0]) > 0 {
		bid, _ = strconv.ParseFloat(payload.Bids[0][0], 64)
	}
	if len(payload.Asks) > 0 && len(payload.Asks[0]) > 0 {
		ask, _ = strconv.ParseFloat(payload.Asks[0][0], 64)
	}

	pd := a.priceCache.UpdateDepth(sym, bid, ask)
	return sym, pd, nil
}

func (a *WsAdapter) ParseTicker(data []byte) (string, *store.PriceData, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(data, &m); err != nil {
		return "", nil, fmt.Errorf("unmarshal map: %w", err)
	}

	var event string
	if val, ok := m["e"]; ok {
		if err := json.Unmarshal(val, &event); err != nil {
			return "", nil, fmt.Errorf("unmarshal event field: %w", err)
		}
	}

	if event == wsChannelDepth {
		return a.parseDepth(data)
	}

	var payload weexWsTickerPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return "", nil, fmt.Errorf("unmarshal ticker payload: %w", err)
	}
	if len(payload.Data) == 0 {
		return "", nil, fmt.Errorf("no ticker data in payload")
	}

	lastPrice, err := payload.Data[0].LastPrice.Float64()
	if err != nil {
		return "", nil, fmt.Errorf("invalid ticker price: %w", err)
	}
	fairPrice, _ := payload.Data[0].MarkPrice.Float64()
	vol24, _ := payload.Data[0].Volume24.Float64()

	sym := strings.ToUpper(payload.Symbol)
	pd := a.priceCache.UpdateTicker(sym, lastPrice, fairPrice, vol24)
	return sym, pd, nil
}

type weexWsPositionItem struct {
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	Size        string `json:"size"`
	Leverage    string `json:"leverage"`
	OpenValue   string `json:"openValue"`
	UpdatedTime string `json:"updatedTime"`
}

type weexWsPositionPayload struct {
	E    string               `json:"e"`
	Time int64                `json:"E"`
	Data []weexWsPositionItem `json:"d"`
}

func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var payload weexWsPositionPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("unmarshal position update: %w", err)
	}
	if len(payload.Data) == 0 {
		return nil, fmt.Errorf("no position data in payload")
	}
	pos := &payload.Data[0]

	vol, _ := strconv.ParseFloat(pos.Size, 64)
	openVal, _ := strconv.ParseFloat(pos.OpenValue, 64)
	lev, _ := strconv.Atoi(pos.Leverage)

	avgPrice := 0.0
	if vol > 0 {
		avgPrice = openVal / vol
	}

	pType := exchange.PositionTypeLong
	if strings.EqualFold(pos.Side, "SHORT") {
		pType = exchange.PositionTypeShort
	}

	uTime, _ := strconv.ParseInt(pos.UpdatedTime, 10, 64)
	if uTime == 0 {
		uTime = payload.Time
	}
	if uTime == 0 {
		uTime = time.Now().UnixMilli()
	}

	return &exchange.PersonalPositionUpdate{
		Symbol:          strings.ToUpper(pos.Symbol),
		HoldVolContract: vol,
		PositionType:    pType,
		OpenAvgPrice:    avgPrice,
		HoldAvgPrice:    avgPrice,
		Leverage:        lev,
		UpdateTime:      uTime,
	}, nil
}
