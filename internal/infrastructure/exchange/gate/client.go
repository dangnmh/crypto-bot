package gate

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/ticker"

	transportlog "github.com/dangnmh/transport"
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

	if logCfg.HTTP && clientCopy.Transport != nil {
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
	if resp.StatusCode >= 300 {
		var apiErr struct {
			Label   string `json:"label"`
			Message string `json:"message"`
			Detail  string `json:"detail"`
		}
		if err := json.Unmarshal(respBody, &apiErr); err == nil && apiErr.Label != "" {
			msg := apiErr.Message
			if apiErr.Detail != "" {
				msg = apiErr.Detail
			}
			return fmt.Errorf("gate.io api error: label=%s message=%s", apiErr.Label, msg)
		}
		return fmt.Errorf("gate.io http error: status=%d body=%s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("failed to unmarshal gate response: %w (body=%s)", err, string(respBody))
		}
	}

	return nil
}

// sendRequest makes a signed HTTP request to Gate.io and parses the response.
func (c *Client) sendRequest(ctx context.Context, method, path string, query url.Values, bodyObj, result any) error {
	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return err
	}
	if query != nil {
		reqURL.RawQuery = query.Encode()
	}

	var bodyBytes []byte
	if bodyObj != nil {
		bodyBytes, err = json.Marshal(bodyObj)
		if err != nil {
			return err
		}
	}

	var bodyReader io.Reader
	if bodyBytes != nil {
		bodyReader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), bodyReader)
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Gate-API-Go-Client/v7")

	c.addAuthHeaders(req, method, reqURL, bodyBytes)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return handleResponse(resp, respBody, result)
}

// WarmUp maintaining connection pool via periodic ping requests (/spot/time).
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.logger.InfoContext(ctx, "🔗 Warming up Gate.io connection pool...", slog.Duration("interval", interval))

	ticker.RunImmediate(ctx, interval, func() bool {
		var result gateSystemTime
		err := c.sendRequest(ctx, http.MethodGet, "/spot/time", nil, nil, &result)
		if err != nil {
			c.logger.DebugContext(ctx, "Gate.io warmup ping failed", slog.Any("error", err))
		}
		return true
	})
}

// Latency measures round-trip time of fetching server time (ms).
func (c *Client) Latency(ctx context.Context) (int64, error) {
	start := time.Now()
	var result gateSystemTime
	err := c.sendRequest(ctx, http.MethodGet, "/spot/time", nil, nil, &result)
	if err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

// SupportLeverageOnOrder returns false since Gate.io doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
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
}

type gateFuturesTicker struct {
	Contract       string `json:"contract"`
	Last           string `json:"last"`
	HighestBid     string `json:"highest_bid"`
	LowestAsk      string `json:"lowest_ask"`
	Volume24h      string `json:"volume_24h"`
	Volume24hQuote string `json:"volume_24h_quote"`
}

type gateFuturesCandlestick struct {
	T   int64  `json:"t"`
	V   int64  `json:"v"`
	O   string `json:"o"`
	C   string `json:"c"`
	H   string `json:"h"`
	L   string `json:"l"`
	Sum string `json:"sum"`
}

type gateOrderBookItem struct {
	P string  `json:"p"`
	S float64 `json:"s"`
}

type gateFuturesOrderBook struct {
	Id   int64               `json:"id"`
	Asks []gateOrderBookItem `json:"asks"`
	Bids []gateOrderBookItem `json:"bids"`
}

type gateFuturesAccount struct {
	Total          string `json:"total"`
	UnrealisedPnl  string `json:"unrealised_pnl"`
	PositionMargin string `json:"position_margin"`
	Available      string `json:"available"`
	Currency       string `json:"currency"`
}

type gatePosition struct {
	Contract   string      `json:"contract"`
	Size       int64       `json:"size"`
	EntryPrice string      `json:"entry_price"`
	Leverage   json.Number `json:"leverage"`
	Mode       string      `json:"mode"`
}

type gatePositionClose struct {
	Time          float64 `json:"time"`
	FirstOpenTime int64   `json:"first_open_time"`
	Pnl           string  `json:"pnl"`
	PnlPnl        string  `json:"pnl_pnl"`
	PnlFund       string  `json:"pnl_fund"`
	PnlFee        string  `json:"pnl_fee"`
	LongPrice     string  `json:"long_price"`
	ShortPrice    string  `json:"short_price"`
	Side          string  `json:"side"`
	Contract      string  `json:"contract"`
	Text          string  `json:"text"`
	AccumSize     string  `json:"accum_size"`
}

type gateFuturesOrder struct {
	Contract   string  `json:"contract"`
	Size       int64   `json:"size"`
	Price      string  `json:"price,omitempty"`
	Close      bool    `json:"close,omitempty"`
	ReduceOnly bool    `json:"reduce_only,omitempty"`
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
