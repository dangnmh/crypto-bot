package bingx

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/decmath"
	pkgws "crypto-bot/pkg/ws"

	"github.com/buger/jsonparser"

	"crypto-bot/pkg/xjson"
)

// WsAdapter implements ws.ExchangeAdapter for BingX Futures.
type WsAdapter struct {
	pool         *pkgws.Pool
	client       *Client
	apiKey       string
	apiSecret    string
	cancelKeep   context.CancelFunc
	cancelKeepMu sync.Mutex
	privateURL   string
}

// NewWsAdapter creates a new BingX WsAdapter.
func NewWsAdapter(privateURL string) *WsAdapter {
	return &WsAdapter{
		privateURL: privateURL,
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

// SubscribeTicker subscribes to both bookTicker and ticker streams.
func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	bookTopic := symbol + "@" + channelBookTicker
	bookMsg := map[string]any{
		"id":          "sub-" + symbol + "-bookticker",
		paramReqType:  opSub,
		paramDataType: bookTopic,
	}
	bookTopicKey := symbol + ":ticker:book"
	if err := a.pool.SubscribePublic(ctx, bookTopicKey, bookMsg); err != nil {
		return err
	}

	tickerTopic := symbol + "@" + channelTicker
	tickerMsg := map[string]any{
		"id":          "sub-" + symbol + "-ticker",
		paramReqType:  opSub,
		paramDataType: tickerTopic,
	}
	marketTopicKey := symbol + ":ticker:market"
	if err := a.pool.SubscribePublic(ctx, marketTopicKey, tickerMsg); err != nil {
		return err
	}

	return nil
}

// UnsubscribeTicker unsubscribes from both bookTicker and ticker streams.
func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	bookTopic := symbol + "@" + channelBookTicker
	bookMsg := map[string]any{
		"id":          "unsub-" + symbol + "-bookticker",
		paramReqType:  opUnsub,
		paramDataType: bookTopic,
	}
	bookTopicKey := symbol + ":ticker:book"
	err1 := a.pool.UnsubscribePublic(ctx, bookTopicKey, bookMsg)

	tickerTopic := symbol + "@" + channelTicker
	tickerMsg := map[string]any{
		"id":          "unsub-" + symbol + "-ticker",
		paramReqType:  opUnsub,
		paramDataType: tickerTopic,
	}
	marketTopicKey := symbol + ":ticker:market"
	err2 := a.pool.UnsubscribePublic(ctx, marketTopicKey, tickerMsg)

	if err1 != nil {
		return err1
	}
	return err2
}

// SubscribePersonal is a placeholder since we only scan public channels.
func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	return nil
}

// GetPingConfig returns application ping config.
func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	return msgPing, 30 * time.Second
}

// GetAuthHook intercepts OnConnected to store credentials and authenticate.
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	a.apiKey = apiKey
	a.apiSecret = apiSecret
	return nil
}

// GetPreprocessor returns decompression function for GZIP payloads.
func (a *WsAdapter) GetPreprocessor() func([]byte) ([]byte, error) {
	return func(data []byte) ([]byte, error) {
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		defer func() { _ = r.Close() }()
		return io.ReadAll(r)
	}
}

// GetChannelExtractor maps BingX events to channels.
func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(data []byte) string {
		return a.extractChannel(data)
	}
}

func (a *WsAdapter) extractChannel(data []byte) string {
	pingData, err := jsonparser.GetString(data, "ping")
	if err == nil && pingData != "" {
		return msgPing
	}

	trimmed := strings.TrimSpace(string(data))
	trimmed = strings.Trim(trimmed, `"`)
	if strings.EqualFold(trimmed, msgPing) {
		return msgPing
	}

	dataType, err := jsonparser.GetString(data, paramDataType)
	if err == nil {
		return a.extractPublicChannel(dataType)
	}

	if a.client != nil && a.client.logger != nil {
		a.client.logger.Debug("GetChannelExtractor", slog.String("data", string(data)))
	}

	// Fallback to event name at root for private user data stream messages
	eventName, err := jsonparser.GetString(data, "e")
	if err == nil {
		if eventName == "ACCOUNT_UPDATE" {
			return "personal.position"
		}
		return eventName
	}

	return ""
}

func (a *WsAdapter) extractPublicChannel(dataType string) string {
	parts := strings.Split(dataType, "@")
	if len(parts) < 2 {
		return ""
	}

	streamType := parts[1]
	if strings.HasPrefix(streamType, channelKline) {
		return channelKline
	}
	if strings.HasPrefix(streamType, channelDepth) {
		return channelDepth
	}
	if streamType == channelTicker || streamType == channelBookTicker {
		return channelTicker
	}

	return dataType
}

// ParseTicker parses ticker feed into store.PriceData.
func (a *WsAdapter) ParseTicker(data []byte) (symbol string, pd *store.PriceData, err error) {
	dataType, err := jsonparser.GetString(data, paramDataType)
	if err != nil {
		return "", nil, err
	}

	parts := strings.Split(dataType, "@")
	if len(parts) < 2 {
		return "", nil, fmt.Errorf("invalid dataType in ticker push: %s", dataType)
	}
	sym := parts[0]
	streamType := parts[1]

	dataNode, _, _, err := jsonparser.Get(data, "data")
	if err != nil {
		return "", nil, err
	}

	var last, bid, ask, vol float64

	if streamType == channelBookTicker {
		type wsBookTicker struct {
			B    string `json:"b"` // Best bid price
			BQty string `json:"B"` // Best bid qty
			A    string `json:"a"` // Best ask price
			AQty string `json:"A"` // Best ask qty
		}
		var raw wsBookTicker
		if err := xjson.Unmarshal(dataNode, &raw); err != nil {
			return "", nil, fmt.Errorf("unmarshal bookTicker push: %w", err)
		}
		bid = decmath.ParseFloat(raw.B)
		ask = decmath.ParseFloat(raw.A)
		vol = decmath.ParseFloat(raw.BQty)
		switch {
		case bid > 0 && ask > 0:
			last = (bid + ask) / 2
		case bid > 0:
			last = bid
		default:
			last = ask
		}
	} else {
		// Handles "@ticker" stream type
		type ws24hTicker struct {
			LastPrice float64 `json:"c"` // Latest transaction price (real API)
			Volume    float64 `json:"v"` // 24-hour volume
			BestBid   float64 `json:"B"` // Best bid price
			BestAsk   float64 `json:"A"` // Best ask price
			// Ignored fields to prevent case-insensitive collision fallback in Go json.Unmarshal
			IgnoredC any `json:"C"` // Matches "C" timestamp exactly
			IgnoredB any `json:"b"` // Matches "b" bid quantity exactly
			IgnoredA any `json:"a"` // Matches "a" ask quantity exactly
		}
		var raw ws24hTicker
		if err := xjson.Unmarshal(dataNode, &raw); err != nil {
			return "", nil, fmt.Errorf("unmarshal ticker push: %w", err)
		}

		last = raw.LastPrice
		bid = raw.BestBid
		ask = raw.BestAsk
		vol = raw.Volume
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
	if err := xjson.Unmarshal(data, &msg); err != nil {
		return nil, err
	}

	if len(msg.Update.Positions) == 0 {
		return nil, fmt.Errorf("empty position update in push")
	}

	var raw = msg.Update.Positions[0]
	for _, item := range msg.Update.Positions {
		if math.Abs(decmath.ParseFloat(item.Amount)) > 0 {
			raw = item
			break
		}
	}

	amt := decmath.ParseFloat(raw.Amount)
	posType := exchange.PositionTypeLong
	if amt < 0 || raw.PositionSide == posSideShort {
		posType = exchange.PositionTypeShort
	}

	update := &exchange.PersonalPositionUpdate{
		Symbol:          raw.Symbol,
		HoldVol:         math.Abs(amt),
		HoldAvgPrice:    decmath.ParseFloat(raw.EntryPrice),
		CloseProfitLoss: decmath.ParseFloat(raw.Unrealized),
		PositionType:    posType,
	}

	return update, nil
}

// GetPrivateURLFunc returns a dynami RL generation function for the private WebSocket connection.
func (a *WsAdapter) GetPrivateURLFunc(ctx context.Context) func() (string, error) {
	return func() (string, error) {
		if a.client == nil {
			return "", fmt.Errorf("bingx REST client not injected in WsAdapter")
		}

		// 1. Fetch new listenKey via REST API
		listenKey, err := a.client.CreateListenKey(ctx)
		if err != nil {
			return "", fmt.Errorf("create bingx listen key failed: %w", err)
		}

		// 2. Prevent goroutine leaks by canceling any previous keepalive loop
		a.cancelKeepMu.Lock()
		if a.cancelKeep != nil {
			a.cancelKeep()
		}
		keepCtx, cancel := context.WithCancel(ctx)
		a.cancelKeep = cancel
		a.cancelKeepMu.Unlock()

		// 3. Start keepalive loop for the new listenKey
		go a.keepAliveLoop(keepCtx, listenKey)

		// 4. Construct private websocket URL
		base := a.privateURL
		if base == "" {
			base = "wss://open-api-swap.bingx.com/swap-market"
		}

		base = strings.TrimSuffix(base, "/")
		return base + "?listenKey=" + listenKey, nil
	}
}

func (a *WsAdapter) keepAliveLoop(ctx context.Context, listenKey string) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()

	a.client.logger.DebugContext(ctx, "⏳ Started keepalive loop for BingX user data stream")

	for {
		select {
		case <-ctx.Done():
			a.client.logger.DebugContext(context.WithoutCancel(ctx), "⏳ Stopped keepalive loop for BingX user data stream")
			return
		case <-ticker.C:
			if err := a.client.KeepAliveListenKey(ctx, listenKey); err != nil {
				a.client.logger.ErrorContext(ctx, "🔴 Failed to keepalive BingX user data stream", slog.Any("error", err))
			} else {
				a.client.logger.DebugContext(ctx, "🟢 Successfully kept alive BingX user data stream")
			}
		}
	}
}
