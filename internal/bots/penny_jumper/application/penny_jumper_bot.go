package application

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	pjstore "crypto-bot/internal/bots/penny_jumper/infrastructure/store"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/store/orderbook"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/ticker"
)

var _ infraapp.Bot = (*PennyJumperBot)(nil)

// PennyJumperBot orchestrates the Penny Jumper trading bot lifecycle.
type PennyJumperBot struct {
	cfg              pjdomain.PennyJumperConfig
	engine           *infraapp.Engine
	notifier         notifier.Notifier
	subscribeManager *SubscribeManager
	runner           *PennyJumperRunner
	depthStores      map[string]*pjstore.DepthStore
	contractStores   map[string]*store.ContractStore
	syncs            map[string]orderbook.Synchronizer
	bus              *eventbus.Bus
	logger           *slog.Logger

	mu         sync.Mutex
	running    bool
	cancelFunc context.CancelFunc
	wg         sync.WaitGroup
}

// NewPennyJumperBot creates a new PennyJumperBot.
func NewPennyJumperBot(
	cfg pjdomain.PennyJumperConfig,
	engine *infraapp.Engine,
	notif notifier.Notifier,
	subscribeManager *SubscribeManager,
	runner *PennyJumperRunner,
	depthStores map[string]*pjstore.DepthStore,
	contractStores map[string]*store.ContractStore,
	syncs map[string]orderbook.Synchronizer,
	bus *eventbus.Bus,
	logger *slog.Logger,
) *PennyJumperBot {
	return &PennyJumperBot{
		cfg:              cfg,
		engine:           engine,
		notifier:         notif,
		subscribeManager: subscribeManager,
		runner:           runner,
		depthStores:      depthStores,
		contractStores:   contractStores,
		syncs:            syncs,
		bus:              bus,
		logger:           logger.With("bot", "penny_jumper"),
	}
}

// Name returns the identifier of the bot.
func (b *PennyJumperBot) Name() string {
	return FlowIDPennyJumper
}

// RunAsBackground implements infraapp.Bot interface.
func (b *PennyJumperBot) RunAsBackground(ctx context.Context) error {
	return b.Start(ctx)
}

// Run implements infraapp.Bot interface blocking until context is cancelled.
func (b *PennyJumperBot) Run(ctx context.Context) error {
	<-ctx.Done()
	return nil
}

// Start boots all background pipelines and listeners.
func (b *PennyJumperBot) Start(ctx context.Context) error {
	b.mu.Lock()
	if b.running {
		b.mu.Unlock()
		return fmt.Errorf("bot is already running")
	}
	b.running = true
	botCtx, cancel := context.WithCancel(ctx)
	b.cancelFunc = cancel
	b.mu.Unlock()

	b.logger.InfoContext(botCtx, "🏁 Starting Penny Jumper Bot (Event-Sourced)",
		slog.Any("exchanges", b.cfg.Exchanges),
		slog.String("mode", string(b.cfg.ExecutionMode)),
	)

	// 1. Wait for contract stores to perform their initial sync (max 5s)
	for exch, cs := range b.contractStores {
		waitCtx, cancelWait := context.WithTimeout(botCtx, 5*time.Second)
		if err := cs.WaitReady(waitCtx); err != nil {
			b.logger.WarnContext(botCtx, "ContractStore initial sync timed out, continuing", slog.String("exchange", exch))
		}
		cancelWait()
	}

	// 2. Register global subscriptions on EventBus
	InitGlobalSubscriptions(botCtx, b.runner)

	// 3. Attach WS depth stream handler
	b.setupDepthStream(botCtx)

	// 4. Attach WS trade stream handler
	b.setupTradeStream(botCtx)

	// 5. Initial Universe Refresh and periodic refresh ticker
	b.wg.Add(1)
	go b.runUniverseJob(botCtx)

	return nil
}

func (b *PennyJumperBot) setupDepthStream(ctx context.Context) {
	for _, exch := range b.cfg.GetExchanges() {
		prov, err := b.engine.GetProvider(exch)
		if err != nil || prov == nil || prov.WSPool == nil {
			continue
		}

		adapter, ok := prov.Adapter.(exchange.DepthParser)
		if !ok {
			b.logger.WarnContext(ctx, "Exchange adapter does not implement DepthParser", slog.String("exchange", exch))
			continue
		}

		syn, hasSyn := b.syncs[exch]
		if !hasSyn || syn == nil {
			b.logger.WarnContext(ctx, "Exchange synchronizer not found", slog.String("exchange", exch))
			continue
		}

		b.logger.InfoContext(ctx, "🔌 Wires depth WebSocket stream handler", slog.String("exchange", exch))
		exchangeName := exch
		prov.WSPool.On("depth", func(data []byte) {
			sym, ob, err := adapter.ParseDepth(data)
			if err != nil || ob == nil {
				return
			}

			err = syn.ProcessUpdate(ctx, ob)
			if err != nil {
				b.logger.Error("sync process update failed",
					slog.String("exchange", exchangeName),
					slog.String("symbol", sym),
					slog.String("error", err.Error()),
				)
			}

			if snap, ok := syn.GetTopN(sym, 20); ok {
				ob = snap
			} else {
				return
			}

			evtTimestamp := ob.Timestamp
			if evtTimestamp.IsZero() {
				evtTimestamp = time.Now().UTC()
			}

			_ = b.bus.Publish(pjdomain.TopicDepthUpdated, pjdomain.DepthUpdatedEvent{
				Exchange:  exchangeName,
				Symbol:    sym,
				Version:   ob.Version,
				OrderBook: ob,
				Timestamp: evtTimestamp,
			})
		})
	}
}

func (b *PennyJumperBot) setupTradeStream(ctx context.Context) {
	for _, exch := range b.cfg.GetExchanges() {
		prov, err := b.engine.GetProvider(exch)
		if err != nil || prov == nil || prov.WSPool == nil {
			continue
		}

		adapter, ok := prov.Adapter.(exchange.TradeParser)
		if !ok {
			b.logger.WarnContext(ctx, "Exchange adapter does not implement TradeParser", slog.String("exchange", exch))
			continue
		}

		ds, hasDS := b.depthStores[exch]
		if !hasDS || ds == nil {
			continue
		}

		b.logger.InfoContext(ctx, "🔌 Wires trade WebSocket stream handler", slog.String("exchange", exch))
		prov.WSPool.On("trade", func(data []byte) {
			sym, trades, err := adapter.ParseTrade(data)
			if err != nil || len(trades) == 0 {
				return
			}

			ds.RecordPublicTrades(sym, trades)
		})
	}
}

func (b *PennyJumperBot) runUniverseJob(ctx context.Context) {
	defer b.wg.Done()

	interval := time.Duration(b.cfg.Universe.TickerInterval)
	ticker.RunImmediate(ctx, interval, func() bool {
		if _, err := b.subscribeManager.RefreshUniverse(ctx); err != nil {
			b.logger.ErrorContext(ctx, "Failed to refresh universe", slog.Any("error", err))
		}
		return true
	})
}

// Stop shuts down the bot gracefully.
func (b *PennyJumperBot) Stop(ctx context.Context) error {
	b.mu.Lock()
	if !b.running {
		b.mu.Unlock()
		return nil
	}
	b.running = false
	if b.cancelFunc != nil {
		b.cancelFunc()
	}
	b.mu.Unlock()

	b.logger.WarnContext(ctx, "🛑 Stopping Penny Jumper Bot")

	b.subscribeManager.UnsubscribeAll(ctx)
	b.wg.Wait()
	return nil
}
