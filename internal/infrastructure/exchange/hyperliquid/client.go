package hyperliquid

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/config"
	applogger "crypto-bot/pkg/logger"
	"crypto-bot/pkg/ticker"

	"github.com/ethereum/go-ethereum/crypto"
	hl "github.com/sonirico/go-hyperliquid"
)

// Client is the Hyperliquid Futures client wrapper.
type Client struct {
	exchange    *hl.Exchange
	info        *hl.Info
	userAddress string
	baseURL     string
	logger      *slog.Logger
}

// NewClient creates a new Hyperliquid API Client.
func NewClient(httpClient *http.Client, baseURL, apiKey, apiSecret string, logCfg config.LoggingConfig) *Client {
	logger := slog.Default().With("component", "exchange").With("exchange", "hyperliquid")

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
			logger.Error("Failed to parse Ethereum private key", "error", err)
		} else {
			userAddress = crypto.PubkeyToAddress(pk.PublicKey).Hex()
			exchangeClient = hl.NewExchange(
				context.Background(),
				pk,
				baseURL,
				meta,     // Meta
				"",       // Vault Address
				"",       // Account Address
				spotMeta, // SpotMeta
				nil,      // PerpDexs
				hl.ExchangeOptClientOptions(hl.ClientOptHTTPClient(httpClient)),
			)
		}
	}

	infoClient := hl.NewInfo(
		context.Background(),
		baseURL,
		true,      // Skip WS in Info client
		meta,      // Meta
		spotMeta,  // SpotMeta
		spotState, // SpotState
		hl.InfoOptClientOptions(hl.ClientOptHTTPClient(httpClient)),
	)

	return &Client{
		exchange:    exchangeClient,
		info:        infoClient,
		userAddress: userAddress,
		baseURL:     baseURL,
		logger:      logger,
	}
}

// WarmUp maintains public connection pool.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	applogger.WithCtx(ctx, c.logger).Info("🔗 Warming up Hyperliquid connection pool...", "interval", interval)

	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.info.AllMids(ctx)
		if err != nil {
			applogger.WithCtx(ctx, c.logger).Debug("Warmup mid prices call failed", "error", err)
		}
		return true
	})
}
