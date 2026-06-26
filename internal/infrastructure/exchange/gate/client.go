package gate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"

	transportlog "github.com/dangnmh/transport"

	"crypto-bot/pkg/xjson"
)

// Client is the Gate.io Perpetual Futures REST API client.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string
	apiSecret  string
	logCfg     config.LoggingConfig
	logger     *slog.Logger
	clock      exchange.Clock
}

// NewClient creates a new Gate.io Perpetual Futures REST API client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "gate")

	clientCopy := *httpClient

	if clientCopy.Transport != nil {
		if logCfg.HTTP {
			rt := clientCopy.Transport
			rt = transportlog.NewTransportLog(rt,
				transportlog.LogOptionLogger(logger),
				transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
					OnStatus:       []int{0},
					WhiteListPaths: []string{"*"}, // match all paths
					BlackListPaths: []string{
						"GET|/api/v4/spot/time",
						"GET|/api/v4/futures/usdt/tickers",
						"GET|/api/v4/futures/usdt/contracts",
					}, // match everything cleanly
				}),
				transportlog.LogOptionRedactSensitive(true),
				transportlog.LogOptionRedactSensitiveKeys([]string{"Key"}),
				transportlog.LogOptionQueryParams(true),
			)
			clientCopy.Transport = rt
		}
		clientCopy.Transport = httpclient.WrapWithRequestID(clientCopy.Transport)
	}

	base := "https://api.gateio.ws/api/v4"
	if baseURL != "" {
		base = strings.TrimRight(baseURL, "/")
	}

	return &Client{
		httpClient: &clientCopy,
		baseURL:    base,
		apiKey:     apiKey,
		apiSecret:  apiSecret,
		logCfg:     logCfg,
		logger:     logger,
		clock:      exchange.RealClock{},
	}
}

// SetClock configures a custom clock implementation.
func (c *Client) SetClock(clk exchange.Clock) {
	if clk != nil {
		c.clock = clk
	}
}

func (c *Client) addAuthHeaders(req *http.Request, method string, reqURL *url.URL, bodyBytes []byte) {
	if c.apiKey == "" || c.apiSecret == "" {
		return
	}
	h := sha512.New()
	if bodyBytes != nil {
		h.Write(bodyBytes)
	}
	hashedPayload := hex.EncodeToString(h.Sum(nil))

	t := strconv.FormatInt(c.clock.Now().Unix(), 10)
	rawQuery, _ := url.QueryUnescape(reqURL.RawQuery)
	msg := fmt.Sprintf("%s\n%s\n%s\n%s\n%s", method, reqURL.Path, rawQuery, hashedPayload, t)
	mac := hmac.New(sha512.New, []byte(c.apiSecret))
	mac.Write([]byte(msg))
	sign := hex.EncodeToString(mac.Sum(nil))

	req.Header.Set("KEY", c.apiKey)
	req.Header.Set("SIGN", sign)
	req.Header.Set("Timestamp", t)
}

func handleResponse(resp *http.Response, respBody []byte, result any) error {
	if resp != nil && resp.StatusCode >= 300 {
		var apiErr struct {
			Label   string `json:"label"`
			Message string `json:"message"`
			Detail  string `json:"detail"`
		}
		if err := xjson.Unmarshal(respBody, &apiErr); err == nil && apiErr.Label != "" {
			msg := apiErr.Message
			if apiErr.Detail != "" {
				msg = apiErr.Detail
			}
			return fmt.Errorf("gate.io api error: label=%s message=%s", apiErr.Label, msg)
		}
		return fmt.Errorf("gate.io http error: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := xjson.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to unmarshal gate response: %w (body=%s)", err, string(respBody))
		}
	}

	return nil
}

// sendRequest makes a signed HTTP request to Gate.io and parses the response.
func (c *Client) sendRequest(ctx context.Context, method, path string, query url.Values, bodyObj, result any) error {
	var queryMap map[string]string
	if query != nil {
		queryMap = make(map[string]string)
		for k, v := range query {
			if len(v) > 0 {
				queryMap[k] = v[0]
			}
		}
	}

	var bodyBytes []byte
	if bodyObj != nil {
		var err error
		bodyBytes, err = xjson.Marshal(bodyObj)
		if err != nil {
			return err
		}
	}

	respBody, err := c.RawRequest(ctx, method, path, queryMap, bodyBytes)
	if err != nil {
		return err
	}

	return handleResponse(nil, respBody, result)
}

// RawRequest makes a signed HTTP request of any method to the Gate.io API.
func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	q := reqURL.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	if len(q) > 0 {
		reqURL.RawQuery = q.Encode()
	}

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Gate-API-Go-Client/v7")

	c.addAuthHeaders(req, method, reqURL, body)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, &exchange.RateLimitError{
				Message: string(respBody),
				Path:    path,
			}
		}
		var apiErr struct {
			Label   string `json:"label"`
			Message string `json:"message"`
			Detail  string `json:"detail"`
		}
		if err := xjson.Unmarshal(respBody, &apiErr); err == nil && apiErr.Label != "" {
			msg := apiErr.Message
			if apiErr.Detail != "" {
				msg = apiErr.Detail
			}
			return nil, &exchange.APIError{
				StatusCode: resp.StatusCode,
				Message:    fmt.Sprintf("label=%s message=%s", apiErr.Label, msg),
				Path:       path,
			}
		}
		return nil, &exchange.APIError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
			Path:       path,
		}
	}

	return respBody, nil
}

// Structs representing Gate.io Futures REST API payloads.

type gateSystemTime struct {
	ServerTime int64 `json:"server_time"`
}

type gateContract struct {
	Name             string  `json:"name"`
	QuantoMultiplier string  `json:"quanto_multiplier"`
	LeverageMin      string  `json:"leverage_min"`
	LeverageMax      string  `json:"leverage_max"`
	OrderPriceRound  string  `json:"order_price_round"`
	MakerFeeRate     string  `json:"maker_fee_rate"`
	TakerFeeRate     string  `json:"taker_fee_rate"`
	FundingRate      string  `json:"funding_rate"`
	FundingNextApply float64 `json:"funding_next_apply"`
	OrderSizeMin     float64 `json:"order_size_min"`
	OrderSizeMax     float64 `json:"order_size_max"`
}

type gateFuturesTicker struct {
	Contract       string `json:"contract"`
	Last           string `json:"last"`
	HighestBid     string `json:"highest_bid"`
	LowestAsk      string `json:"lowest_ask"`
	Volume24h      string `json:"volume_24h"`
	Volume24hQuote string `json:"volume_24h_quote"`
}

type gatePosition struct {
	Contract   string       `json:"contract"`
	Size       xjson.Number `json:"size"`
	EntryPrice string       `json:"entry_price"`
	Leverage   xjson.Number `json:"leverage"`
	Mode       string       `json:"mode"`
	//nolint:misspell // Gate.io API uses the British spelling realised_pnl.
	RealisedPnl  string `json:"realised_pnl"`
	PnlPnl       string `json:"pnl_pnl"`
	PnlFund      string `json:"pnl_fund"`
	PnlFee       string `json:"pnl_fee"`
	HistoryPnl   string `json:"history_pnl"`
	LastClosePnl string `json:"last_close_pnl"`
	UpdateTime   int64  `json:"update_time"`
	OpenTime     int64  `json:"open_time"`
}

type gateMyTrade struct {
	ID         int64        `json:"id"`
	CreateTime float64      `json:"create_time"`
	Contract   string       `json:"contract"`
	OrderID    string       `json:"order_id"`
	Size       xjson.Number `json:"size"`
	CloseSize  xjson.Number `json:"close_size"`
	Price      xjson.Number `json:"price"`
	Role       string       `json:"role"`
	Text       string       `json:"text"`
	Fee        xjson.Number `json:"fee"`
	PointFee   xjson.Number `json:"point_fee"`
	TradeValue string       `json:"trade_value"`
}

type gateAccountBook struct {
	Time    float64 `json:"time"`
	Change  string  `json:"change"`
	Balance string  `json:"balance"`
	Type    string  `json:"type"`
	Text    string  `json:"text"`
}

type gateFuturesOrder struct {
	Contract   string  `json:"contract"`
	Size       int64   `json:"size"`
	Price      string  `json:"price,omitempty"`
	Close      *bool   `json:"close,omitempty"`
	ReduceOnly *bool   `json:"reduce_only,omitempty"`
	Tif        string  `json:"tif,omitempty"`
	Text       string  `json:"text,omitempty"`
	AutoSize   string  `json:"auto_size,omitempty"`
	Id         int64   `json:"id,omitempty"`
	CreateTime float64 `json:"create_time,omitempty"`
	FinishTime float64 `json:"finish_time,omitempty"`
	FinishAs   string  `json:"finish_as,omitempty"`
	Status     string  `json:"status,omitempty"`
	Left       int64   `json:"left,omitempty"`
	FillPrice  string  `json:"fill_price,omitempty"`
	// TP/SL fields
	TpslTpTriggerPrice string `json:"tpsl_tp_trigger_price,omitempty"`
	TpslSlTriggerPrice string `json:"tpsl_sl_trigger_price,omitempty"`
	TpslTpPriceType    string `json:"tpsl_tp_price_type,omitempty"`
	TpslSlPriceType    string `json:"tpsl_sl_price_type,omitempty"`
}

func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	settle := params["settle"]
	if settle == "" {
		settle = gateSettleUsdt
	}
	path := fmt.Sprintf("/futures/%s/tickers", settle)
	return c.RawRequest(ctx, http.MethodGet, path, params, nil)
}

func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	settle := params["settle"]
	if settle == "" {
		settle = gateSettleUsdt
	}
	path := fmt.Sprintf("/futures/%s/tickers", settle)
	return c.RawRequest(ctx, http.MethodGet, path, params, nil)
}

func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	settle := params["settle"]
	if settle == "" {
		settle = gateSettleUsdt
	}
	path := fmt.Sprintf("/futures/%s/positions", settle)
	return c.RawRequest(ctx, http.MethodGet, path, params, nil)
}

func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	settle := params["settle"]
	if settle == "" {
		settle = gateSettleUsdt
	}
	path := fmt.Sprintf("/futures/%s/my_trades", settle)
	return c.RawRequest(ctx, http.MethodGet, path, params, nil)
}

func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	settle := params["settle"]
	if settle == "" {
		settle = gateSettleUsdt
	}
	path := fmt.Sprintf("/futures/%s/orders/%s", settle, orderID)
	return c.RawRequest(ctx, http.MethodGet, path, params, nil)
}

func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	settle := p["settle"]
	if settle == "" {
		settle = gateSettleUsdt
	}
	if p["status"] == "" {
		p["status"] = "finished"
	}
	path := fmt.Sprintf("/futures/%s/orders", settle)
	return c.RawRequest(ctx, http.MethodGet, path, p, nil)
}

func (c *Client) GetOrderDealsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	settle := params["settle"]
	if settle == "" {
		settle = gateSettleUsdt
	}
	path := fmt.Sprintf("/futures/%s/my_trades", settle)
	return c.RawRequest(ctx, http.MethodGet, path, params, nil)
}

func (c *Client) GetAccountBook(ctx context.Context, params map[string]string) ([]byte, error) {
	settle := params["settle"]
	if settle == "" {
		settle = gateSettleUsdt
	}
	path := fmt.Sprintf("/futures/%s/account_book", settle)
	return c.RawRequest(ctx, http.MethodGet, path, params, nil)
}

func (c *Client) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	symbol := params["symbol"]
	orderID := params["order_id"]
	if orderID == "" {
		orderID = params["orderId"]
	}
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required")
	}
	if orderID == "" {
		return nil, fmt.Errorf("order_id is required")
	}
	info, err := c.GetOrderPNL(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return xjson.Marshal(info)
}
