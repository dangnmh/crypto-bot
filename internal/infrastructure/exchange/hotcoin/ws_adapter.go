package hotcoin

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"slices"
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

// WsAdapter implements ws.ExchangeAdapter for Hotcoin.
type WsAdapter struct {
	pool       *pkgws.Pool
	client     *Client
	apiKey     string
	apiSecret  string
	clock      exchange.Clock
	priceCache *infraws.PriceCache
	symbols    []string
	symMu      sync.Mutex
}

// NewWsAdapter creates a new WsAdapter.
func NewWsAdapter(apiKey, apiSecret string) *WsAdapter {
	return &WsAdapter{
		apiKey:     apiKey,
		apiSecret:  apiSecret,
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

func (a *WsAdapter) sendSubscription(ctx context.Context, action, symbol string) error {
	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))
	tickerChannel := fmt.Sprintf("market.%s.ticker", contractCode)
	tickerMsg := map[string]any{
		eventKey: action,
		paramsKey: map[string]any{
			bizKey:          perpetualBiz,
			typeKey:         tickerType,
			contractCodeKey: contractCode,
			serializeKey:    false,
		},
	}

	if action == subscribeEvent {
		return a.pool.SubscribePublic(ctx, tickerChannel, tickerMsg)
	}
	return a.pool.UnsubscribePublic(ctx, tickerChannel, tickerMsg)
}

// SubscribeTicker subscribes to live price tickers and orderbook depth streams.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	a.symMu.Lock()
	exists := slices.Contains(a.symbols, symbol)
	if !exists {
		a.symbols = append(a.symbols, symbol)
	}
	a.symMu.Unlock()
	return a.sendSubscription(ctx, subscribeEvent, symbol)
}

// UnsubscribeTicker unsubscribes from tickers and depth.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	return a.sendSubscription(ctx, unsubscribeEvent, symbol)
}

// SubscribePersonal subscribes to position and fill channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	posMsg := map[string]any{
		eventKey: subscribeEvent,
		paramsKey: map[string]any{
			bizKey:       perpetualBiz,
			typeKey:      positionsType, // "position"
			zipParam:     false,
			serializeKey: false,
		},
	}
	if err := a.pool.SendPrivate(ctx, posMsg); err != nil {
		return err
	}

	fillsMsg := map[string]any{
		eventKey: subscribeEvent,
		paramsKey: map[string]any{
			bizKey:       perpetualBiz,
			typeKey:      fillsType,
			zipParam:     false,
			serializeKey: false,
		},
	}
	if err := a.pool.SendPrivate(ctx, fillsMsg); err != nil {
		return err
	}
	return nil
}

func (a *WsAdapter) UnsubscribePersonal(ctx context.Context) error {
	return nil
}

// GetPingConfig returns client-initiated ping configuration (event=ping) every 30 seconds.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	pingMsg := map[string]any{
		eventKey: pingValue,
	}
	return pingMsg, 30 * time.Second
}

// GetCustomPingHandler returns a custom handler for incoming server-side ping messages.
func (a *WsAdapter) GetCustomPingHandler() func(*websocket.Conn, []byte) bool {
	return func(conn *websocket.Conn, data []byte) bool {
		var ping struct {
			Ping any `json:"ping"`
		}
		if err := json.Unmarshal(data, &ping); err == nil && ping.Ping != nil {
			var pongVal any
			if s, ok := ping.Ping.(string); ok {
				if s == pingValue {
					pongVal = pongValue
				} else {
					pongVal = s
				}
			} else {
				pongVal = ping.Ping
			}

			pongMsg := map[string]any{
				pongValue: pongVal,
			}
			pongBytes, _ := json.Marshal(pongMsg)
			if conn != nil {
				_ = conn.WriteMessage(websocket.TextMessage, pongBytes)
			}
			return true
		}
		return false
	}
}

// GetPreprocessor decompresses GZIP compressed WebSocket frames.
func (a *WsAdapter) GetPreprocessor() func([]byte) ([]byte, error) {
	return func(data []byte) ([]byte, error) {
		if len(data) < 2 {
			return data, nil
		}
		// check magic headers of gzip (0x1f, 0x8b)
		if data[0] == 0x1f && data[1] == 0x8b {
			r, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				return nil, err
			}
			defer func() { _ = r.Close() }()
			return io.ReadAll(r)
		}
		return data, nil
	}
}

func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	if apiKey != "" {
		a.apiKey = apiKey
	}
	if apiSecret != "" {
		a.apiSecret = apiSecret
	}
	return func(client *pkgws.Client) {
		timestamp := strconv.FormatInt(a.clock.Now().UnixMilli(), 10)

		// Sort and build query params
		params := url.Values{}
		params.Set("AccessKeyId", a.apiKey)
		params.Set("SignatureMethod", "HmacSHA256")
		params.Set("SignatureVersion", "2")
		params.Set("Timestamp", timestamp)

		tempParams := params.Encode()
		payload := fmt.Sprintf("WSS\napi.ws.contract.hotcoin.top\n/wss\n%s", tempParams)

		mac := hmac.New(sha256.New, []byte(a.apiSecret))
		mac.Write([]byte(payload))
		signature := base64.StdEncoding.EncodeToString(mac.Sum(nil))

		authMsg := map[string]any{
			"event": signinEvent,
			"params": map[string]string{
				"apiKey":    a.apiKey,
				"timestamp": timestamp,
				"signature": signature,
			},
		}
		if err := client.SendJSON(authMsg); err != nil {
			slog.Error("Hotcoin WS auth failed to send signin message", "error", err)
		}
	}
}

func parseChannelName(channel string) string {
	if strings.HasPrefix(channel, "market.") && (strings.HasSuffix(channel, ".ticker") || strings.HasSuffix(channel, ".depth")) {
		return wsChannelTicker
	}
	if channel == personalPosition || channel == positionsType || channel == positionsPlural {
		return personalPosition
	}
	if channel == personalOrder || channel == fillsType {
		return personalOrder
	}
	return channel
}

func parseEventName(event string) string {
	if event == positionsType || event == positionsPlural {
		return personalPosition
	}
	if event == fillsType {
		return personalOrder
	}
	return event
}

func extractChannelByField(channel, event, status string) string {
	if status == "ok" || event == subscribeEvent || event == signinEvent {
		return keySubscribed
	}

	if channel != "" {
		return parseChannelName(channel)
	}

	if event != "" {
		return parseEventName(event)
	}

	return ""
}

// GetChannelExtractor routing callback.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		var msg struct {
			Channel      string `json:"channel"`
			Event        string `json:"event"`
			Status       string `json:"status"`
			Biz          string `json:"biz"`
			Type         string `json:"type"`
			ContractCode string `json:"contractCode"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			return ""
		}
		if msg.Event == subscribeEvent || msg.Event == unsubscribeEvent || msg.Channel == subscribeEvent || msg.Channel == unsubscribeEvent {
			return keySubscribed
		}
		if msg.Biz == "perpetual" {
			if msg.Type == "ticker" || msg.Type == "depth" {
				return wsChannelTicker
			}
			if msg.Type == positionsType || msg.Type == positionsPlural {
				return personalPosition
			}
			if msg.Type == fillsType {
				return personalOrder
			}
		}
		return extractChannelByField(msg.Channel, msg.Event, msg.Status)
	}
}

type hotcoinWsTickerData struct {
	Ask        xjson.Number `json:"ask"`
	Bid        xjson.Number `json:"bid"`
	LastPrice  xjson.Number `json:"lastPrice"`
	BaseVolume xjson.Number `json:"baseVolume"`
	MarkPrice  xjson.Number `json:"markPrice"`
}

type hotcoinWsTickerMsg struct {
	Channel string              `json:"channel"`
	Data    hotcoinWsTickerData `json:"data"`
}

type hotcoinWsDepthMsg struct {
	Channel string `json:"channel"`
	Data    struct {
		Bids [][]string `json:"bids"`
		Asks [][]string `json:"asks"`
	} `json:"data"`
}

type hotcoinWsNewTickerMsg struct {
	Biz          string           `json:"biz"`
	Type         string           `json:"type"`
	ContractCode string           `json:"contractCode"`
	Data         [][]xjson.Number `json:"data"`
}

// ParseTicker parses public ticker/depth messages.
func (a *WsAdapter) ParseTicker(data []byte) (string, *store.PriceData, error) {
	var generic struct {
		Channel      string `json:"channel"`
		Biz          string `json:"biz"`
		Type         string `json:"type"`
		ContractCode string `json:"contractCode"`
	}
	if err := json.Unmarshal(data, &generic); err != nil {
		return "", nil, err
	}

	var symbolCode string
	var isDepth bool
	var isNewTicker bool

	if generic.Biz == perpetualBiz {
		symbolCode = generic.ContractCode
		switch generic.Type {
		case tickerType:
			isNewTicker = true
		case depthType:
			isDepth = true
		}
	} else {
		parts := strings.Split(generic.Channel, ".")
		if len(parts) < 3 {
			return "", nil, fmt.Errorf("invalid channel format: %s", generic.Channel)
		}
		symbolCode = parts[1]
		isDepth = strings.HasSuffix(generic.Channel, ".depth")
	}

	symbol := strings.ToUpper(symbolCode)
	if !strings.Contains(symbol, "_") {
		if before, ok := strings.CutSuffix(symbol, "USDT"); ok {
			symbol = before + "_USDT"
		} else if before, ok := strings.CutSuffix(symbol, "USDC"); ok {
			symbol = before + "_USDC"
		}
	}

	if isNewTicker {
		return a.parseNewTicker(symbol, data)
	}
	if isDepth {
		return a.parseLegacyDepth(symbol, data)
	}
	return a.parseLegacyTicker(symbol, data)
}

func (a *WsAdapter) parseNewTicker(symbol string, data []byte) (string, *store.PriceData, error) {
	var tickerMsg hotcoinWsNewTickerMsg
	if err := json.Unmarshal(data, &tickerMsg); err != nil {
		return "", nil, err
	}
	if len(tickerMsg.Data) == 0 {
		return "", nil, fmt.Errorf("empty ticker data array")
	}
	row := tickerMsg.Data[0]
	if len(row) < 11 {
		return "", nil, fmt.Errorf("invalid ticker data row length: %d", len(row))
	}

	last := xjson.ToFloat64(row[6])
	vol := xjson.ToFloat64(row[3])
	bid := xjson.ToFloat64(row[9])
	ask := xjson.ToFloat64(row[10])

	var mark float64
	if bid > 0 && ask > 0 {
		mark = (bid + ask) / 2
	} else {
		mark = last
	}

	pd := a.priceCache.UpdateTicker(symbol, last, mark, vol)
	if bid > 0 && ask > 0 {
		pd = a.priceCache.UpdateDepth(symbol, bid, ask)
	}
	return symbol, pd, nil
}

func (a *WsAdapter) parseLegacyDepth(symbol string, data []byte) (string, *store.PriceData, error) {
	var depth hotcoinWsDepthMsg
	if err := json.Unmarshal(data, &depth); err != nil {
		return "", nil, err
	}
	var bid, ask float64
	if len(depth.Data.Bids) > 0 && len(depth.Data.Bids[0]) > 0 {
		bid, _ = strconv.ParseFloat(depth.Data.Bids[0][0], 64)
	}
	if len(depth.Data.Asks) > 0 && len(depth.Data.Asks[0]) > 0 {
		ask, _ = strconv.ParseFloat(depth.Data.Asks[0][0], 64)
	}
	pd := a.priceCache.UpdateDepth(symbol, bid, ask)
	return symbol, pd, nil
}

func (a *WsAdapter) parseLegacyTicker(symbol string, data []byte) (string, *store.PriceData, error) {
	var tickerMsg hotcoinWsTickerMsg
	if err := json.Unmarshal(data, &tickerMsg); err != nil {
		return "", nil, err
	}

	last := xjson.ToFloat64(tickerMsg.Data.LastPrice)
	mark := xjson.ToFloat64(tickerMsg.Data.MarkPrice)
	vol := xjson.ToFloat64(tickerMsg.Data.BaseVolume)

	pd := a.priceCache.UpdateTicker(symbol, last, mark, vol)
	return symbol, pd, nil
}

type hotcoinWsPositionItem struct {
	ContractCode string       `json:"contractCode"`
	Side         string       `json:"side"`
	HoldVolume   xjson.Number `json:"holdVolume"`
	OpenAvgPrice xjson.Number `json:"openAvgPrice"`
	Leverage     xjson.Number `json:"leverage"`
}

type hotcoinWsPositionMsg struct {
	Event string                `json:"event"`
	Data  hotcoinWsPositionItem `json:"data"`
}

// ParsePosition parses private position push messages.
func (a *WsAdapter) ParsePosition(data []byte) (*exchange.PersonalPositionUpdate, error) {
	var msg hotcoinWsPositionMsg
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	pType := exchange.PositionTypeLong
	if strings.EqualFold(msg.Data.Side, "short") {
		pType = exchange.PositionTypeShort
	}

	symbol := strings.ToUpper(msg.Data.ContractCode)
	if !strings.Contains(symbol, "_") {
		if before, ok := strings.CutSuffix(symbol, "USDT"); ok {
			symbol = before + "_USDT"
		} else if before, ok := strings.CutSuffix(symbol, "USDC"); ok {
			symbol = before + "_USDC"
		}
	}

	avgPrice := xjson.ToFloat64(msg.Data.OpenAvgPrice)
	vol := xjson.ToFloat64(msg.Data.HoldVolume)
	lev := int(xjson.ToInt64(msg.Data.Leverage))

	return &exchange.PersonalPositionUpdate{
		Symbol:          symbol,
		HoldVolContract: vol,
		PositionType:    pType,
		OpenAvgPrice:    avgPrice,
		HoldAvgPrice:    avgPrice,
		Leverage:        lev,
		UpdateTime:      time.Now().UnixMilli(),
	}, nil
}

// ParseDepth parses depth messages into domain.OrderBook.
func (a *WsAdapter) ParseDepth(data []byte) (string, *domain.OrderBook, error) {
	return "", nil, nil
}
