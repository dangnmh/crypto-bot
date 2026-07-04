package pionex

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	pkgws "crypto-bot/pkg/ws"
	"crypto-bot/pkg/xjson"

	"github.com/gorilla/websocket"
)

type WsAdapter struct {
	client     *Client
	pool       *pkgws.Pool
	privateURL string
}

func NewWsAdapter(client *Client, privateURL string) *WsAdapter {
	return &WsAdapter{
		client:     client,
		privateURL: privateURL,
	}
}

func (a *WsAdapter) GetPrivateURLFunc(ctx context.Context) func() (string, error) {
	return func() (string, error) {
		if a.client.apiKey == "" || a.client.apiSecret == "" {
			return a.privateURL, nil
		}

		u, err := url.Parse(a.privateURL)
		if err != nil {
			return "", err
		}

		timestamp := strconv.FormatInt(a.client.clock.Now().UnixMilli(), 10)

		// 1. Prepare key-value pairs sorted in ascending ASCII order (key then timestamp)
		queryString := fmt.Sprintf("key=%s&timestamp=%s", a.client.apiKey, timestamp)

		// 2. Base path: path (e.g. /wsUA) + ? + query parameters
		basePath := u.Path + "?" + queryString

		// 3. Append the literal string "websocket_auth"
		signStr := basePath + "websocket_auth"

		// 4. Generate HMAC SHA256 using API Secret
		h := hmac.New(sha256.New, []byte(a.client.apiSecret))
		h.Write([]byte(signStr))
		signature := hex.EncodeToString(h.Sum(nil))

		// 5. Build final connection URL query params
		q := u.Query()
		q.Set("key", a.client.apiKey)
		q.Set("timestamp", timestamp)
		q.Set("signature", signature)
		u.RawQuery = q.Encode()

		return u.String(), nil
	}
}

func (a *WsAdapter) SetPool(pool *pkgws.Pool) {
	a.pool = pool
}

func (a *WsAdapter) GetPingConfig() (any, time.Duration) {
	// Periodic ping to keep connection alive
	return "ping", 30 * time.Second
}

func (a *WsAdapter) GetCustomPingHandler() func(*websocket.Conn, []byte) bool {
	return func(conn *websocket.Conn, data []byte) bool {
		var envelope struct {
			Op        string `json:"op"`
			Timestamp int64  `json:"timestamp"`
		}
		if err := xjson.Unmarshal(data, &envelope); err == nil {
			if strings.EqualFold(envelope.Op, "PING") {
				pongMsg := map[string]any{
					"op":        "PONG",
					"timestamp": envelope.Timestamp,
				}
				pongBytes, _ := xjson.Marshal(pongMsg)
				_ = conn.WriteMessage(websocket.TextMessage, pongBytes)
				return true
			}
		}
		str := string(data)
		if strings.EqualFold(str, "ping") {
			_ = conn.WriteMessage(websocket.TextMessage, []byte("PONG"))
			return true
		}
		return false
	}
}

func (a *WsAdapter) GetChannelExtractor() func([]byte) string {
	return func(msg []byte) string {
		var envelope struct {
			Topic string `json:"topic"`
			Op    string `json:"op"`
		}
		_ = xjson.Unmarshal(msg, &envelope)
		if envelope.Op != "" {
			return strings.ToUpper(envelope.Op)
		}
		if envelope.Topic != "" {
			return strings.ToUpper(envelope.Topic)
		}
		return ""
	}
}

func (a *WsAdapter) SubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		"op":      "SUBSCRIBE",
		topicKey:  depthTopic,
		symbolKey: symbol,
		limitKey:  5,
	}
	return a.pool.SubscribePublic(ctx, symbol, msg)
}

func (a *WsAdapter) UnsubscribeTicker(ctx context.Context, symbol string) error {
	msg := map[string]any{
		"op":      "UNSUBSCRIBE",
		topicKey:  depthTopic,
		symbolKey: symbol,
		limitKey:  5,
	}
	return a.pool.UnsubscribePublic(ctx, symbol, msg)
}

type pionexWsDepthData struct {
	Bids [][]string `json:"bids"`
	Asks [][]string `json:"asks"`
}

type pionexWsDepthMessage struct {
	Topic     string            `json:"topic"`
	Symbol    string            `json:"symbol"`
	Data      pionexWsDepthData `json:"data"`
	Timestamp int64             `json:"timestamp"`
}

func (a *WsAdapter) ParseTicker(msg []byte) (string, *store.PriceData, error) {
	var envelope pionexWsDepthMessage
	if err := xjson.Unmarshal(msg, &envelope); err != nil {
		return "", nil, err
	}

	var bid, ask float64
	if len(envelope.Data.Bids) > 0 && len(envelope.Data.Bids[0]) > 0 {
		bid, _ = strconv.ParseFloat(envelope.Data.Bids[0][0], 64)
	}
	if len(envelope.Data.Asks) > 0 && len(envelope.Data.Asks[0]) > 0 {
		ask, _ = strconv.ParseFloat(envelope.Data.Asks[0][0], 64)
	}

	mid := 0.0
	if bid > 0 && ask > 0 {
		mid = (bid + ask) / 2.0
	}

	return envelope.Symbol, &store.PriceData{
		BestBid:   bid,
		BestAsk:   ask,
		LastPrice: mid,
		UpdatedAt: time.UnixMilli(envelope.Timestamp),
	}, nil
}

// GetAuthHook returns client authentication hook (Phase 1).
func (a *WsAdapter) GetAuthHook(apiKey, apiSecret string) func(*pkgws.Client) {
	return nil
}

func (a *WsAdapter) SubscribePersonal(ctx context.Context) error {
	return nil
}

func (a *WsAdapter) ParsePosition(msg []byte) (*exchange.PersonalPositionUpdate, error) {
	return nil, nil
}

func (a *WsAdapter) ParseOrder(msg []byte) (*exchange.WsOrderDeal, error) {
	return nil, nil
}
