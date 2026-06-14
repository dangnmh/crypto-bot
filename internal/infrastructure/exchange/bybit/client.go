package bybit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/ticker"

	transportlog "github.com/dangnmh/transport"
)

// Client is the Bybit V5 Perpetual Futures REST API client.
type Client struct {
	httpClient  *http.Client
	baseURL     string
	apiKey      string
	apiSecret   string
	accountType string // "standard" or "unified"
	logCfg      config.LoggingConfig
	logger      *slog.Logger
	clock       exchange.Clock
}

// NewClient creates a new Bybit client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret, accountType string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "bybit")

	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}

	if httpClient != nil && clientCopy.Transport != nil {
		if logCfg.HTTP {
			rt := clientCopy.Transport
			rt = transportlog.NewTransportLog(rt,
				transportlog.LogOptionLogger(logger),
				transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
					OnStatus:       []int{0},
					WhiteListPaths: []string{"*"}, // match all paths
					BlackListPaths: []string{
						"GET|/v5/market/tickers",
						"GET|/v5/market/time",
						"GET|/v5/market/instruments-info",
					}, // match everything cleanly
				}),
				transportlog.LogOptionRedactSensitive(true),
				transportlog.LogOptionRedactSensitiveKeys([]string{"X-Bapi-Api-Key"}),
				transportlog.LogOptionQueryParams(true),
			)
			clientCopy.Transport = rt
		}
		clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)
	}

	return &Client{
		httpClient:  &clientCopy,
		baseURL:     baseURL,
		apiKey:      apiKey,
		apiSecret:   apiSecret,
		accountType: accountType,
		logCfg:      logCfg,
		logger:      logger,
		clock:       exchange.RealClock{},
	}
}

// SetClock configures a custom clock implementation.
func (c *Client) SetClock(clk exchange.Clock) {
	if clk != nil {
		c.clock = clk
	}
}

// WarmUp maintains the connection pool.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.GetServerTime(ctx)
		if err != nil {
			c.logger.Debug("Bybit warmup ping failed", slog.Any("error", err))
		}
		return true
	})
}

// SupportLeverageOnOrder returns true since Bybit V5 supports set-leverage inside create order request.
func (c *Client) SupportLeverageOnOrder() bool {
	return true
}

func (c *Client) buildRequestPayload(method string, params any) (io.Reader, []byte, string, error) {
	if method == http.MethodGet {
		if params == nil {
			return nil, nil, "", nil
		}
		var paramsMap map[string]any
		if m, ok := params.(map[string]any); ok {
			paramsMap = m
		} else {
			paramsMap = structToMap(params)
		}
		if len(paramsMap) == 0 {
			return nil, nil, "", nil
		}
		vals := url.Values{}
		for k, v := range paramsMap {
			vals.Set(k, fmt.Sprintf("%v", v))
		}
		return nil, nil, vals.Encode(), nil
	}

	if params == nil {
		return nil, nil, "", nil
	}
	bodyBytes, err := json.Marshal(params)
	if err != nil {
		return nil, nil, "", fmt.Errorf("marshal request body: %w", err)
	}
	return bytes.NewReader(bodyBytes), bodyBytes, "", nil
}

func (c *Client) signRequest(method string, bodyBytes []byte, queryString string) (string, string) {
	ts := strconv.FormatInt(c.clock.Now().UnixMilli(), 10)
	recvWindow := "5000"

	var signatureBase string
	if method == http.MethodPost {
		signatureBase = ts + c.apiKey + recvWindow + string(bodyBytes)
	} else {
		signatureBase = ts + c.apiKey + recvWindow + queryString
	}

	hmac256 := hmac.New(sha256.New, []byte(c.apiSecret))
	hmac256.Write([]byte(signatureBase))
	signature := hex.EncodeToString(hmac256.Sum(nil))

	return ts, signature
}

func (c *Client) sendRequest(ctx context.Context, method, path string, params any, isSigned bool) ([]byte, error) {
	bodyReader, bodyBytes, queryString, err := c.buildRequestPayload(method, params)
	if err != nil {
		return nil, err
	}

	urlPath := c.baseURL + path
	if method == http.MethodGet && queryString != "" {
		urlPath += "?" + queryString
	}

	req, err := http.NewRequestWithContext(ctx, method, urlPath, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("User-Agent", "bybit.api.go/1.0.7")
	if method != http.MethodGet && bodyBytes != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if isSigned {
		ts, signature := c.signRequest(method, bodyBytes, queryString)
		req.Header.Set("X-BAPI-API-KEY", c.apiKey)
		req.Header.Set("X-BAPI-SIGN-TYPE", "2")
		req.Header.Set("X-BAPI-TIMESTAMP", ts)
		req.Header.Set("X-BAPI-RECV-WINDOW", "5000")
		req.Header.Set("X-BAPI-SIGN", signature)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode >= http.StatusBadRequest {
		return nil, fmt.Errorf("HTTP error: status=%d, body=%s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

type bybitResponse[T any] struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  T      `json:"result"`
}

func parseResponse[T any](body []byte, errPrefix string) (T, error) {
	var resp bybitResponse[T]
	if err := json.Unmarshal(body, &resp); err != nil {
		var zero T
		return zero, fmt.Errorf("%s json unmarshal: %w", errPrefix, err)
	}
	if resp.RetCode != 0 {
		var zero T
		return zero, fmt.Errorf("%s error: retCode=%d, retMsg=%s", errPrefix, resp.RetCode, resp.RetMsg)
	}
	return resp.Result, nil
}

func decodeListResponse[T any](body []byte, errPrefix string) ([]T, error) {
	var resp bybitResponse[struct {
		List []T `json:"list"`
	}]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("%s json unmarshal: %w", errPrefix, err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("%s error: retCode=%d, retMsg=%s", errPrefix, resp.RetCode, resp.RetMsg)
	}
	return resp.Result.List, nil
}
