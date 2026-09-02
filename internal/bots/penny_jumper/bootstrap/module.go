package bootstrap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"crypto-bot/internal/bots/penny_jumper/application"
	pjconfig "crypto-bot/internal/bots/penny_jumper/config"
	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	"crypto-bot/internal/bots/penny_jumper/infrastructure/ai"
	"crypto-bot/internal/bots/penny_jumper/infrastructure/persistence"
	pjstore "crypto-bot/internal/bots/penny_jumper/infrastructure/store"
	shared "crypto-bot/internal/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	sysconfig "crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/observability"
	"crypto-bot/internal/infrastructure/server"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/store/orderbook"
	"crypto-bot/pkg/eventbus"
	applogger "crypto-bot/pkg/logger"

	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"gorm.io/gorm"
)

// ConfigPaths holds paths to CLI configuration files.
type ConfigPaths struct {
	System    string
	Exchange  string
	Bot       string
	Blacklist string
}

// Module wires Penny Jumper bot dependencies and lifecycle.
func Module(paths ConfigPaths) fx.Option {
	return fx.Options(
		fx.Supply(paths),
		exchange.Module,
		notifier.Module,
		observability.Module,
		server.Module,
		infraapp.Module,
		fx.Provide(
			provideSystemConfig,
			provideBaseSystemConfig,
			provideLogger,
			providePennyJumperConfigAndBlacklist,
			providePennyJumperConfig,
			provideBlacklist,
			provideNotifierConfig,
			provideClock,
			provideDatabase,
			provideWallRepository,
			provideEventBus,
			provideEngine,
			provideOrderBookSynchronizers,
			provideDepthStores,
			provideContractStores,
			provideWallDetectors,
			provideWallJudge,
			providePennyJumperRunner,
			provideSubscribeManager,
			providePennyJumperBot,
		),
		fx.WithLogger(func(log *slog.Logger) fxevent.Logger {
			return &fxevent.SlogLogger{Logger: log.With("component", "fx")}
		}),
	)
}

func provideSystemConfig(paths ConfigPaths) (*pjconfig.SystemConfig, error) {
	return pjconfig.LoadSystemConfig(paths.System, paths.Exchange)
}

func provideBaseSystemConfig(sysCfg *pjconfig.SystemConfig) *sysconfig.SystemConfig {
	return &sysCfg.SystemConfig
}

func provideLogger(lc fx.Lifecycle, cfg *pjconfig.SystemConfig) *slog.Logger {
	cleanup := applogger.InitLogger(cfg.Logging.Level, cfg.Env)
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			cleanup()
			return nil
		},
	})
	return slog.Default().With("service", "penny_jumper")
}

type PJConfigResult struct {
	Config    *pjdomain.PennyJumperConfig
	Blacklist []string
}

func providePennyJumperConfigAndBlacklist(paths ConfigPaths) (PJConfigResult, error) {
	cfg, blk, err := pjconfig.LoadPennyJumperConfig(paths.Bot, paths.Blacklist)
	if err != nil {
		return PJConfigResult{}, err
	}
	return PJConfigResult{Config: cfg, Blacklist: blk}, nil
}

func providePennyJumperConfig(res PJConfigResult) *pjdomain.PennyJumperConfig {
	return res.Config
}

func provideBlacklist(res PJConfigResult) []string {
	return res.Blacklist
}

func provideNotifierConfig(cfg *pjconfig.SystemConfig) notifier.Config {
	return notifier.Config{
		Enabled:                cfg.NotiConfig.TelegramBotToken != "",
		TelegramBotToken:       cfg.NotiConfig.TelegramBotToken,
		TelegramChatID:         cfg.NotiConfig.TelegramChatID,
		TelegramCriticalChatID: cfg.NotiConfig.TelegramCriticalChatID,
	}
}

func provideClock() shared.Clock {
	return shared.SystemClock{}
}

func provideDatabase(lc fx.Lifecycle) (*gorm.DB, error) {
	return infraapp.InitDatabase(lc, &persistence.PennyJumperWallRecord{})
}

func provideWallRepository(db *gorm.DB) persistence.WallRepository {
	return persistence.NewGormWallRepository(db)
}

func provideEventBus(logger *slog.Logger) *eventbus.Bus {
	return eventbus.New(logger)
}

func provideEngine(
	cfg *pjconfig.SystemConfig,
	botCfg *pjdomain.PennyJumperConfig,
	httpClient *http.Client,
	log *slog.Logger,
) (*infraapp.Engine, error) {
	activeExchanges := botCfg.GetExchanges()
	return infraapp.NewEngine(context.Background(), infraapp.EngineConfig{
		SystemConfig:     &cfg.SystemConfig,
		HTTPClient:       httpClient,
		Logger:           log,
		ActiveExchanges:  activeExchanges,
		TimeSyncInterval: 30 * time.Second,
	})
}

func provideDepthStores(cfg *pjdomain.PennyJumperConfig) map[string]*pjstore.DepthStore {
	exchanges := cfg.GetExchanges()
	stores := make(map[string]*pjstore.DepthStore, len(exchanges))
	for _, exch := range exchanges {
		stores[exch] = pjstore.NewDepthStore(30*time.Minute, 5*time.Minute)
	}
	return stores
}

func provideContractStores(
	lc fx.Lifecycle,
	cfg *pjdomain.PennyJumperConfig,
	engine *infraapp.Engine,
	logger *slog.Logger,
) map[string]*store.ContractStore {
	exchanges := cfg.GetExchanges()
	stores := make(map[string]*store.ContractStore, len(exchanges))
	ctx, cancel := context.WithCancel(context.Background())
	lc.Append(fx.Hook{
		OnStop: func(context.Context) error {
			cancel()
			return nil
		},
	})
	for _, exch := range exchanges {
		cs := store.NewContractStore(nil, logger)
		if engine != nil {
			if prov, err := engine.GetProvider(exch); err == nil && prov != nil && prov.Client != nil {
				go cs.StartContractSync(ctx, prov.Client, 1*time.Hour)
			}
		}
		stores[exch] = cs
	}
	return stores
}

func provideWallDetectors(
	cfg *pjdomain.PennyJumperConfig,
	depthStores map[string]*pjstore.DepthStore,
	contractStores map[string]*store.ContractStore,
	bus *eventbus.Bus,
	logger *slog.Logger,
) map[string]*application.WallDetector {
	exchanges := cfg.GetExchanges()
	detectors := make(map[string]*application.WallDetector, len(exchanges))
	for _, exch := range exchanges {
		detectors[exch] = application.NewWallDetector(
			exch,
			cfg.WallDetector,
			depthStores[exch],
			contractStores[exch],
			bus,
			logger,
		)
	}
	return detectors
}

func provideWallJudge(
	cfg *pjdomain.PennyJumperConfig,
	httpClient *http.Client,
	logger *slog.Logger,
) (pjdomain.WallJudge, error) {
	localJudge := pjdomain.NewDefaultWallJudge(pjdomain.DefaultWallJudgeConfig{
		MinTrustScore: cfg.WallJudge.MinTrustScore,
	})

	modelTimeout := time.Duration(cfg.WallJudge.Timeout)
	switch cfg.WallJudge.Mode {
	case "local":
		return localJudge, nil
	case "model":
		modelJudge, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
			Endpoint:      cfg.WallJudge.Endpoint,
			APIKey:        cfg.WallJudge.ApiKey,
			ModelName:     cfg.WallJudge.ModelName,
			Timeout:       modelTimeout,
			MinTrustScore: cfg.WallJudge.MinTrustScore,
		}, httpClient, logger)
		if err != nil {
			return nil, fmt.Errorf("initialize ollama wall judge: %w", err)
		}
		return modelJudge, nil
	default: // "dual"
		modelJudge, err := ai.NewOllamaWallJudge(ai.OllamaWallJudgeConfig{
			Endpoint:      cfg.WallJudge.Endpoint,
			APIKey:        cfg.WallJudge.ApiKey,
			ModelName:     cfg.WallJudge.ModelName,
			Timeout:       modelTimeout,
			MinTrustScore: cfg.WallJudge.MinTrustScore,
		}, httpClient, logger)
		if err != nil {
			logger.Warn("Failed to initialize Ollama Wall Judge in dual mode; running local judge only", "error", err)
			return localJudge, nil
		}
		return pjdomain.NewDualWallJudge(localJudge, modelJudge, modelTimeout, logger), nil
	}
}

func providePennyJumperRunner(
	cfg *pjdomain.PennyJumperConfig,
	depthStores map[string]*pjstore.DepthStore,
	wallDetectors map[string]*application.WallDetector,
	wallJudge pjdomain.WallJudge,
	wallRepo persistence.WallRepository,
	contractStores map[string]*store.ContractStore,
	notif notifier.Notifier,
	bus *eventbus.Bus,
	logger *slog.Logger,
) (*application.PennyJumperRunner, error) {
	return application.NewPennyJumperRunner(
		*cfg,
		depthStores,
		wallDetectors,
		wallJudge,
		wallRepo,
		contractStores,
		notif,
		bus,
		logger,
	)
}

func provideOrderBookSynchronizers(
	cfg *pjdomain.PennyJumperConfig,
	engine *infraapp.Engine,
	logger *slog.Logger,
) (map[string]orderbook.Synchronizer, error) {
	exchanges := cfg.GetExchanges()
	syncs := make(map[string]orderbook.Synchronizer, len(exchanges))

	for _, exch := range exchanges {
		exchCfg, ok := cfg.OrderBookSync.Exchanges[exch]
		if !ok {
			return nil, fmt.Errorf("missing orderBookSync config for exchange: %s", exch)
		}

		var snapProvider exchange.DepthProvider
		var commitsProvider exchange.DepthCommitsProvider

		if engine != nil {
			prov, err := engine.GetProvider(exch)
			if err == nil && prov != nil {
				if p, ok := prov.Client.(exchange.DepthProvider); ok {
					snapProvider = p
				}
				if cp, ok := prov.Client.(exchange.DepthCommitsProvider); ok {
					commitsProvider = cp
				}
			}
		}

		syncCfg := orderbook.SynchronizerConfig{
			Exchange:           exch,
			Mode:               orderbook.SyncMode(exchCfg.Mode),
			StrictSequence:     exchCfg.StrictSequence,
			MaxBufferCapacity:  cfg.OrderBookSync.MaxBufferCapacity,
			SnapshotTimeout:    time.Duration(cfg.OrderBookSync.SnapshotTimeout),
			CommitRecoverySize: cfg.OrderBookSync.CommitRecoverySize,
		}

		syncs[exch] = orderbook.NewSynchronizer(syncCfg, snapProvider, commitsProvider, logger)
	}

	return syncs, nil
}

func provideSubscribeManager(
	cfg *pjdomain.PennyJumperConfig,
	engine *infraapp.Engine,
	syncs map[string]orderbook.Synchronizer,
	depthStores map[string]*pjstore.DepthStore,
	blacklist []string,
	logger *slog.Logger,
) (*application.SubscribeManager, error) {
	exchanges := cfg.GetExchanges()
	clients := make([]application.ExchangeClient, 0, len(exchanges))

	for _, exch := range exchanges {
		prov, err := engine.GetProvider(exch)
		if err != nil || prov == nil || prov.Client == nil {
			return nil, fmt.Errorf("exchange provider for %s not found: %w", exch, err)
		}

		subscriber, _ := prov.Adapter.(application.DepthSubscriber)
		clients = append(clients, application.ExchangeClient{
			Exchange:     exch,
			Fetcher:      prov.Client,
			Subscriber:   subscriber,
			Synchronizer: syncs[exch],
			DepthStore:   depthStores[exch],
		})
	}

	return application.NewSubscribeManager(
		*cfg,
		clients,
		blacklist,
		logger,
	)
}

func providePennyJumperBot(
	cfg *pjdomain.PennyJumperConfig,
	engine *infraapp.Engine,
	notif notifier.Notifier,
	subMgr *application.SubscribeManager,
	runner *application.PennyJumperRunner,
	depthStores map[string]*pjstore.DepthStore,
	contractStores map[string]*store.ContractStore,
	syncs map[string]orderbook.Synchronizer,
	bus *eventbus.Bus,
	logger *slog.Logger,
) infraapp.Bot {
	return application.NewPennyJumperBot(
		*cfg,
		engine,
		notif,
		subMgr,
		runner,
		depthStores,
		contractStores,
		syncs,
		bus,
		logger,
	)
}
