package orangex

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/xjson"

	"github.com/google/uuid"

	transportlog "github.com/dangnmh/transport"
)

const exchangeName = "orangex"

type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logger     *slog.Logger
	clock      exchange.Clock

	// Token cache fields
	tokenMu       sync.RWMutex
	tokenVal      string
	tokenExpiry   int64 // Unix timestamp in seconds
	refresherOnce sync.Once
}

func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", exchangeName)
	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}
	if clientCopy.Transport == nil {
		clientCopy.Transport = http.DefaultTransport
	}

	if logCfg.HTTP {
		rt := clientCopy.Transport
		rt = transportlog.NewTransportLog(rt,
			transportlog.LogOptionLogger(logger),
			transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
				OnStatus:       []int{0},
				WhiteListPaths: []string{"*"},
				BlackListPaths: []string{
					"GET|/public/time",
					"GET|/public/coin_gecko_contracts",
					"GET|/public/tickers",
					"POST|/api/v1/public/get_instruments",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{paramAccessToken, "client_secret", "signature", "Authorization"}),
			transportlog.LogOptionQueryParams(true),
		)
		clientCopy.Transport = rt
	}
	clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)

	return &Client{
		httpClient: &clientCopy,
		baseURL:    strings.TrimRight(baseURL, "/"),
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		logger:     logger,
		clock:      exchange.RealClock{},
	}
}

func (c *Client) SetClock(clk exchange.Clock) {
	if clk != nil {
		c.clock = clk
	}
}

type orangexRPCRequest struct {
	JsonRpc string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type orangexRPCResponse[T any] struct {
	JsonRpc string        `json:"jsonrpc"`
	ID      xjson.Number  `json:"id"`
	Result  T             `json:"result"`
	Error   *orangexError `json:"error,omitempty"`
}

type orangexError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *orangexError) Error() string {
	return fmt.Sprintf("OrangeX error %d: %s", e.Code, e.Message)
}

func (c *Client) postRPC(ctx context.Context, path, method string, params any, signed bool) ([]byte, error) {
	reqBody := orangexRPCRequest{
		JsonRpc: rpcVersion,
		ID:      time.Now().UnixNano(),
		Method:  method,
		Params:  params,
	}
	bodyBytes, err := xjson.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	if signed {
		token, err := c.GetAccessToken(ctx)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http POST %s: %w", path, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusUnauthorized {
			c.tokenMu.Lock()
			c.tokenExpiry = 0 // Invalidate cache
			c.tokenMu.Unlock()
		}
		return nil, fmt.Errorf("http error %d: %s", resp.StatusCode, string(body))
	}

	return body, nil
}

func (c *Client) GetAccessToken(ctx context.Context) (string, error) {
	c.tokenMu.RLock()
	valid := c.tokenVal != "" && c.clock.Now().Unix() < c.tokenExpiry-60
	c.tokenMu.RUnlock()

	if valid {
		c.tokenMu.RLock()
		token := c.tokenVal
		c.tokenMu.RUnlock()
		return token, nil
	}

	c.tokenMu.Lock()
	defer c.tokenMu.Unlock()

	// Double check lock
	if c.tokenVal != "" && c.clock.Now().Unix() < c.tokenExpiry-60 {
		return c.tokenVal, nil
	}

	if err := c.refreshToken(ctx); err != nil {
		return "", err
	}
	return c.tokenVal, nil
}

type authParams struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret,omitempty"`
	Timestamp    int64  `json:"timestamp,omitempty"`
	Nonce        string `json:"nonce,omitempty"`
	Signature    string `json:"signature,omitempty"`
}

type authResult struct {
	AccessToken string `json:"access_token"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (c *Client) refreshToken(ctx context.Context) error {
	timestampMs := c.clock.Now().UnixMilli()
	timestampStr := fmt.Sprintf("%d", timestampMs)
	nonce := uuid.New().String()
	stringToSign := fmt.Sprintf("%s\n%s\n%s\n", c.apiKey, timestampStr, nonce)

	h := hmac.New(sha256.New, []byte(c.apiSecret))
	h.Write([]byte(stringToSign))
	signature := hex.EncodeToString(h.Sum(nil))

	params := authParams{
		GrantType: "client_signature",
		ClientID:  c.apiKey,
		Timestamp: timestampMs,
		Nonce:     nonce,
		Signature: signature,
	}

	respBytes, err := c.postRPC(ctx, "/public/auth", "/public/auth", params, false)
	if err == nil {
		var envelope orangexRPCResponse[authResult]
		if err := xjson.Unmarshal(respBytes, &envelope); err == nil && envelope.Error == nil {
			c.tokenVal = envelope.Result.AccessToken
			c.tokenExpiry = c.clock.Now().Unix() + envelope.Result.ExpiresIn
			return nil
		} else if envelope.Error != nil {
			c.logger.WarnContext(ctx, "OrangeX client_signature auth error", "code", envelope.Error.Code, "message", envelope.Error.Message)
		}
	} else {
		c.logger.WarnContext(ctx, "OrangeX client_signature request failed", "error", err)
	}

	// Fallback to client_credentials
	c.logger.InfoContext(ctx, "Trying client_credentials authentication fallback...")
	fallbackParams := authParams{
		GrantType:    "client_credentials",
		ClientID:     c.apiKey,
		ClientSecret: c.apiSecret,
	}

	respBytes, err = c.postRPC(ctx, "/public/auth", "/public/auth", fallbackParams, false)
	if err != nil {
		return fmt.Errorf("auth post (client_credentials): %w", err)
	}

	var envelope orangexRPCResponse[authResult]
	if err := xjson.Unmarshal(respBytes, &envelope); err != nil {
		return fmt.Errorf("unmarshal auth: %w", err)
	}
	if envelope.Error != nil {
		return envelope.Error
	}

	c.tokenVal = envelope.Result.AccessToken
	c.tokenExpiry = c.clock.Now().Unix() + envelope.Result.ExpiresIn
	return nil
}

func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	var params any
	if len(body) > 0 {
		_ = xjson.Unmarshal(body, &params)
	} else if len(query) > 0 {
		params = query
	}
	return c.postRPC(ctx, path, path, params, true)
}

func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, "POST", "/public/get_instruments", params, nil)
}

func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, "POST", "/public/tickers", params, nil)
}

func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, "POST", "/private/get_positions", params, nil)
}

func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	p := map[string]string{paramOrderID: orderID}
	return c.RawRequest(ctx, "POST", "/private/get_order_state", p, nil)
}

func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, "POST", "/private/get_order_history_by_instrument", params, nil)
}

func (c *Client) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, "POST", "/private/get_user_trades_by_order", params, nil)
}
