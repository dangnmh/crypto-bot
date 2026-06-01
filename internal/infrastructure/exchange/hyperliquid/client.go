package hyperliquid

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/pkg/ticker"

	transportlog "github.com/dangnmh/transport"
	"github.com/ethereum/go-ethereum/crypto"
	hl "github.com/sonirico/go-hyperliquid"
)

// Client is the Hyperliquid Futures client wrapper.
type Client struct {
	exchange    *hl.Exchange
	info        *hl.Info
	httpClient  *http.Client
	userAddress string
	baseURL     string
	logger      *slog.Logger
}

// NewClient creates a new Hyperliquid API Client.
func NewClient(ctx context.Context, httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "hyperliquid")

	var clientCopy http.Client
	if httpClient != nil {
		clientCopy = *httpClient
	}

	if logCfg.HTTP && httpClient != nil && clientCopy.Transport != nil {
		rt := clientCopy.Transport
		rt = transportlog.NewTransportLog(rt,
			transportlog.LogOptionLogger(logger),
			transportlog.LogOptionMatcherConfig(transportlog.MatcherConfig{
				OnStatus:       []int{0},
				WhiteListPaths: []string{"*"}, // match all paths
				BlackListPaths: []string{},    // match everything cleanly
			}),
			transportlog.LogOptionRedactSensitive(true),
			transportlog.LogOptionRedactSensitiveKeys([]string{"ApiKey"}),
			transportlog.LogOptionQueryParams(true),
		)
		clientCopy.Transport = rt
	}

	var finalClient *http.Client
	if httpClient != nil {
		finalClient = &clientCopy
	} else {
		finalClient = &http.Client{}
	}

	var meta *hl.Meta
	var spotMeta *hl.SpotMeta
	var spotState *hl.MixedArray

	if strings.HasPrefix(baseURL, "http://") || strings.Contains(baseURL, "127.0.0.1") {
		meta = &hl.Meta{
			Universe: []hl.AssetInfo{
				{
					Name:        assetBtc,
					SzDecimals:  4,
					MaxLeverage: 50,
				},
			},
		}
		spotMeta = &hl.SpotMeta{}
		spotState = &hl.MixedArray{}
	}

	var userAddress string
	var exchangeClient *hl.Exchange
	if apiSecret != "" {
		pk, err := crypto.HexToECDSA(strings.TrimPrefix(apiSecret, "0x"))
		if err != nil {
			logger.Error("Failed to parse Ethereum private key", slog.Any("error", err))
		} else {
			userAddress = crypto.PubkeyToAddress(pk.PublicKey).Hex()
			exchangeClient = hl.NewExchange(
				ctx,
				pk,
				baseURL,
				meta,     // Meta
				"",       // Vault Address
				"",       // Account Address
				spotMeta, // SpotMeta
				nil,      // PerpDexs
				hl.ExchangeOptClientOptions(hl.ClientOptHTTPClient(finalClient)),
			)
		}
	}

	infoClient := hl.NewInfo(
		ctx,
		baseURL,
		true,      // Skip WS in Info client
		meta,      // Meta
		spotMeta,  // SpotMeta
		spotState, // SpotState
		hl.InfoOptClientOptions(hl.ClientOptHTTPClient(finalClient)),
	)

	return &Client{
		exchange:    exchangeClient,
		info:        infoClient,
		httpClient:  finalClient,
		userAddress: userAddress,
		baseURL:     baseURL,
		logger:      logger,
	}
}

// WarmUp maintains public connection pool.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.logger.InfoContext(ctx, "🔗 Warming up Hyperliquid connection pool...", slog.Duration("interval", interval))

	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.info.AllMids(ctx)
		if err != nil {
			c.logger.DebugContext(ctx, "Warmup mid prices call failed", slog.Any("error", err))
		}
		return true
	})
}

// SupportLeverageOnOrder returns false since Hyperliquid doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}
