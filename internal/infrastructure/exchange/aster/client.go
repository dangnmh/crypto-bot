package aster

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/httpclient"

	transportlog "github.com/dangnmh/transport"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/signer/core/apitypes"
)

type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string // maps to "signer" (API Wallet Address)
	apiSecret  string // maps to "privateKey" (API Private Key Hex)
	passphrase string // maps to "user" (Main Account Wallet Address)
	logger     *slog.Logger
	logCfg     config.LoggingConfig
	clock      exchange.Clock
}

func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret, passphrase string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "aster")
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
					"GET|/fapi/v3/time",
					"GET|/fapi/v3/ticker/24hr",
					"GET|/fapi/v3/ticker/bookTicker",
					"GET|/fapi/v3/premiumIndex",
					"GET|/fapi/v3/exchangeInfo",
					"POST|/fapi/v3/listenKey",
				},
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{"signer", "user", "signature"}),
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
		passphrase: passphrase,
		logger:     logger,
		logCfg:     logCfg,
		clock:      exchange.RealClock{},
	}
}

func (c *Client) SetClock(clk exchange.Clock) {
	if clk != nil {
		c.clock = clk
	}
}

func (c *Client) request(ctx context.Context, method, path string, params map[string]string, signed bool) ([]byte, error) {
	reqURL, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	reqParams := make(map[string]string)
	maps.Copy(reqParams, params)

	if signed {
		reqParams["signer"] = c.apiKey
		reqParams["user"] = c.passphrase
		reqParams["nonce"] = strconv.FormatInt(c.clock.Now().UnixNano()/1000, 10) // microseconds

		sig, err := c.signParams(reqParams)
		if err != nil {
			return nil, fmt.Errorf("sign params: %w", err)
		}
		reqParams["signature"] = sig
	}

	var bodyReader io.Reader
	var queryString string

	if method == http.MethodGet {
		queryString = buildQuery(reqParams)
		reqURL.RawQuery = queryString
	} else {
		form := url.Values{}
		for k, v := range useParams(reqParams) {
			form.Set(k, v)
		}
		bodyReader = strings.NewReader(form.Encode())
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if method != http.MethodGet {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http execute %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var wrapErr struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	hasWrapErr := json.Unmarshal(respBody, &wrapErr) == nil && wrapErr.Code != 0

	if resp.StatusCode != http.StatusOK {
		apiErr := &exchange.APIError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
			Path:       path,
		}
		if hasWrapErr {
			apiErr.Code = wrapErr.Code
			apiErr.Message = wrapErr.Msg
		}
		return nil, apiErr
	}

	if hasWrapErr && wrapErr.Code != 200 {
		return nil, &exchange.APIError{
			Code:    wrapErr.Code,
			Message: wrapErr.Msg,
			Path:    path,
		}
	}

	return respBody, nil
}

func useParams(p map[string]string) map[string]string {
	return p
}

func buildQuery(params map[string]string) string {
	if len(params) == 0 {
		return ""
	}
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var pairs []string
	for _, k := range keys {
		pairs = append(pairs, url.QueryEscape(k)+"="+url.QueryEscape(params[k]))
	}
	return strings.Join(pairs, "&")
}

func (c *Client) signParams(params map[string]string) (string, error) {
	msgStr := buildQuery(params)

	const (
		typeString         = "string"
		msgKey             = "msg"
		primaryTypeMessage = "Message"
	)

	typedData := apitypes.TypedData{
		Types: apitypes.Types{
			"EIP712Domain": []apitypes.Type{
				{Name: "name", Type: typeString},
				{Name: "version", Type: typeString},
				{Name: "chainId", Type: "uint256"},
				{Name: "verifyingContract", Type: "address"},
			},
			primaryTypeMessage: []apitypes.Type{
				{Name: msgKey, Type: typeString},
			},
		},
		PrimaryType: primaryTypeMessage,
		Domain: apitypes.TypedDataDomain{
			Name:              "AsterSignTransaction",
			Version:           "1",
			ChainId:           math.NewHexOrDecimal256(1666),
			VerifyingContract: "0x0000000000000000000000000000000000000000",
		},
		Message: apitypes.TypedDataMessage{
			msgKey: msgStr,
		},
	}

	hash, _, err := apitypes.TypedDataAndHash(typedData)
	if err != nil {
		return "", err
	}

	pk, err := crypto.HexToECDSA(strings.TrimPrefix(c.apiSecret, "0x"))
	if err != nil {
		return "", fmt.Errorf("invalid private key: %w", err)
	}

	sigBytes, err := crypto.Sign(hash, pk)
	if err != nil {
		return "", fmt.Errorf("sign hash: %w", err)
	}

	sigBytes[64] += 27
	return hex.EncodeToString(sigBytes), nil
}

func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	params := make(map[string]string)
	maps.Copy(params, query)
	if len(body) > 0 {
		var temp map[string]any
		if err := json.Unmarshal(body, &temp); err == nil {
			for k, v := range temp {
				params[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	signed := c.apiKey != "" && !strings.Contains(path, "/time") && !strings.Contains(path, "/exchangeInfo") && !strings.Contains(path, "/premiumIndex") && !strings.Contains(path, "/ticker/")
	return c.request(ctx, method, path, params, signed)
}
