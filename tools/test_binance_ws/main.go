package main

import (
	"context"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	fundingconfig "crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/exchange/binance"
	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/logger"
	pkgws "crypto-bot/pkg/ws"
)

func main() {
	sysCfgPath := flag.String("sys", "./configs/funding/system.jsonc", "path to system config")
	symbol := flag.String("symbol", "BTCUSDT", "trading pair symbol to subscribe to")
	apiKeyOverride := flag.String("api-key", "", "override Binance API Key")
	apiSecretOverride := flag.String("api-secret", "", "override Binance API Secret")
	publicURLOverride := flag.String("public-url", "", "override Binance Public WS URL")
	privateURLOverride := flag.String("private-url", "", "override Binance Private WS URL")
	flag.Parse()

	// Init slog for debugging
	cleanup := logger.InitLogger("debug")
	defer cleanup()

	slog.Info("Loading system configuration...", slog.String("path", *sysCfgPath))
	sysCfg, err := loadConfig(*sysCfgPath, *apiKeyOverride, *apiSecretOverride, *publicURLOverride, *privateURLOverride)
	if err != nil {
		slog.Error("Failed to load config", slog.Any("error", err))
		os.Exit(1)
	}

	apiCfg := sysCfg.ExchangeConfig.Binance

	// Validate credentials
	if apiCfg.APIKey == "" || apiCfg.APISecret == "" {
		slog.Error("Binance credentials missing. Please set BINANCE_API_KEY and BINANCE_API_SECRET in environment/Bitwarden or use -api-key and -api-secret flags.")
		os.Exit(1)
	}

	slog.Info("Successfully loaded Binance configuration",
		slog.String("baseURL", apiCfg.Future.BaseURL),
		slog.String("publicWS", apiCfg.WebSocket.PublicEndpoint()),
		slog.String("privateWS", apiCfg.WebSocket.PrivateEndpoint()),
		slog.String("apiKey", maskString(apiCfg.APIKey)),
	)

	httpClient := httpclient.NewPool(httpclient.DefaultPoolConfig())

	// Background context for connection lifecycle
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	adapter, wsPool := setupAdapterAndPool(ctx, sysCfg, httpClient)

	// Register routing and output callbacks
	registerCallbacks(adapter, wsPool)

	slog.Info("Connecting WebSocket Pool...")
	wsPool.Connect(ctx)

	slog.Info("Waiting for private connection (User Data Stream) to be ready...")
	waitCtx, waitCancel := context.WithTimeout(ctx, 30*time.Second)
	defer waitCancel()
	if err := wsPool.WaitReady(waitCtx); err != nil {
		slog.Error("Failed to authenticate or connect private WS", slog.Any("error", err))
		os.Exit(1)
	}
	slog.Info("🟢 Private WebSocket connection is authenticated and ready!")

	// Subscribe to public topics
	subscribePublicChannels(ctx, adapter, *symbol)

	// Keep running until OS signal is received
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	slog.Info("📡 WebSocket testing script is actively running. Press Ctrl+C to exit.")
	sig := <-sigChan
	slog.Info("Received shutdown signal, clean up started...", slog.String("signal", sig.String()))

	// Graceful unsubscribe
	slog.Info("Unsubscribing from channels...")
	if err := adapter.UnsubscribeTicker(context.Background(), *symbol); err != nil {
		slog.Warn("Failed to unsubscribe from ticker", slog.Any("error", err))
	}

	// Close pool
	slog.Info("Closing WebSocket pool connections...")
	wsPool.Close()
	slog.Info("🟢 Clean exit complete.")
}

func loadConfig(sysCfgPath, apiKeyOverride, apiSecretOverride, publicURLOverride, privateURLOverride string) (*fundingconfig.SystemConfig, error) {
	sysCfg, err := fundingconfig.LoadSystemConfig(sysCfgPath)
	if err != nil {
		return nil, err
	}

	if apiKeyOverride != "" {
		sysCfg.ExchangeConfig.Binance.APIKey = apiKeyOverride
	}
	if apiSecretOverride != "" {
		sysCfg.ExchangeConfig.Binance.APISecret = apiSecretOverride
	}

	if publicURLOverride != "" {
		sysCfg.ExchangeConfig.Binance.WebSocket.PublicURL = publicURLOverride
	}
	if privateURLOverride != "" {
		sysCfg.ExchangeConfig.Binance.WebSocket.PrivateURL = privateURLOverride
	}

	return sysCfg, nil
}

func setupAdapterAndPool(ctx context.Context, sysCfg *fundingconfig.SystemConfig, httpClient *http.Client) (*binance.WsAdapter, *pkgws.Pool) {
	apiCfg := sysCfg.ExchangeConfig.Binance
	binanceClient := binance.NewClient(
		httpClient,
		apiCfg.Future.BaseURL,
		apiCfg.APIKey,
		apiCfg.APISecret,
		sysCfg.Logging,
	)

	adapter := binance.NewWsAdapter(apiCfg.WebSocket.PrivateEndpoint())
	adapter.SetClient(binanceClient)

	wsLogger := slog.Default().With("subsystem", "websocket", "exchange", "binance")
	publicOpts := []pkgws.ClientOption{}
	privateOpts := []pkgws.ClientOption{}
	appendCommonOpt := func(opt pkgws.ClientOption) {
		publicOpts = append(publicOpts, opt)
		privateOpts = append(privateOpts, opt)
	}

	if payload, interval := adapter.GetPingConfig(); payload != nil && interval > 0 {
		appendCommonOpt(pkgws.WithPing(payload, interval))
	}
	if extractor := adapter.GetChannelExtractor(); extractor != nil {
		appendCommonOpt(pkgws.WithChannelExtractor(extractor))
	}

	privateOpts = append(privateOpts, pkgws.WithURLFunc(adapter.GetPrivateURLFunc(ctx)))

	wsPool := pkgws.NewPoolWithURLs(
		apiCfg.WebSocket.PublicEndpoint(),
		apiCfg.WebSocket.PrivateEndpoint(),
		apiCfg.WebSocket.MaxPairsPerWSConn,
		wsLogger,
		publicOpts,
		privateOpts,
	)
	adapter.SetPool(wsPool)

	return adapter, wsPool
}

func registerCallbacks(adapter *binance.WsAdapter, wsPool *pkgws.Pool) {
	// 1. Ticker callback
	wsPool.On("ticker", func(data []byte) {
		symbol, pd, err := adapter.ParseTicker(data)
		if err != nil {
			slog.Warn("Failed to parse ticker payload", slog.Any("error", err), slog.String("raw", string(data)))
			return
		}
		slog.Info("📈 [TICKER UPDATE]",
			slog.String("symbol", symbol),
			slog.Float64("price", pd.LastPrice),
			slog.Float64("bid", pd.BestBid),
			slog.Float64("ask", pd.BestAsk),
			slog.Float64("volume24", pd.Volume24),
			slog.Time("updated_at", pd.UpdatedAt),
		)
	})

	// 5. Personal Position Update callback
	wsPool.On("personal.position", func(data []byte) {
		pos, err := adapter.ParsePosition(data)
		if err != nil {
			slog.Warn("Failed to parse position payload", slog.Any("error", err), slog.String("raw", string(data)))
			return
		}
		slog.Info("💼 [POSITION UPDATE]",
			slog.String("symbol", pos.Symbol),
			slog.Float64("hold_vol", pos.HoldVol),
			slog.Float64("entry_price", pos.HoldAvgPrice),
			slog.Float64("unrealized_pnl", pos.CloseProfitLoss),
			slog.Int("position_type", pos.PositionType),
		)
	})
}

func subscribePublicChannels(ctx context.Context, adapter *binance.WsAdapter, symbol string) {
	slog.Info("Subscribing to public channels...", slog.String("symbol", symbol))
	if err := adapter.SubscribeTicker(ctx, symbol); err != nil {
		slog.Error("Failed to subscribe to ticker", slog.Any("error", err))
	} else {
		slog.Info("Subscribed to ticker stream")
	}
}

func maskString(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "..." + s[len(s)-4:]
}
