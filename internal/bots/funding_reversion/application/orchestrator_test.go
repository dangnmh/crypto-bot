package application_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding_reversion/application"
	"crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/testutil/mocks"

	"go.uber.org/mock/gomock"
)

func TestCycleOrchestrator_HappyPath(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	tickerStore := mocks.NewMockTickerReader(ctrl)
	contractStore := mocks.NewMockContractReader(ctrl)
	priceStore := mocks.NewMockPriceReader(ctrl)
	fundingStore := mocks.NewMockFundingReader(ctrl)
	klineStore := mocks.NewMockKlineReadWriter(ctrl)
	depthStore := mocks.NewMockDepthReader(ctrl)
	subscriber := mocks.NewMockSubscriber(ctrl)
	clock := mocks.NewMockClock(ctrl)
	client := mocks.NewMockClient(ctrl)
	ws := mocks.NewMockOrderNotifier(ctrl)

	// onScan
	tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.005,
		LastPrice:   50000,
		BestBid:     49999,
		BestAsk:     50001,
	}, nil).AnyTimes()
	contractStore.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		PriceUnit:    0.1,
		VolUnit:      1,
		MinVol:       1,
		ContractSize: 0.001,
		TakerFeeRate: 0.0006,
	}, nil).AnyTimes()

	// onArm
	subscriber.EXPECT().SubscribeTicker("BTC_USDT").Return(nil).AnyTimes()
	priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()

	// onWait (uses clock.Until)
	clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()

	// onRecheck (uses tickerStore again, handled by AnyTimes above)

	// onFireIOC
	clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	clock.EXPECT().Offset().Return(int64(0)).AnyTimes()

	depthStore.EXPECT().GetDepth(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	clock.EXPECT().Sleep(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, d time.Duration) error {
		<-ctx.Done()
		return ctx.Err()
	}).AnyTimes()

	client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("ioc_1", nil).AnyTimes()
	ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(orderID string, duration time.Duration, callback func(exchange.WsOrderDeal)) {
		go func() {
			callback(exchange.WsOrderDeal{
				OrderID:      orderID,
				State:        exchange.OrderStateFilled,
				DealVol:      1.0,
				DealAvgPrice: 50000,
			})
		}()
	}).AnyTimes()

	ws.EXPECT().RemoveOrderCallback(gomock.Any()).AnyTimes()

	// onFireTrap
	client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("trap_1", nil).AnyTimes()

	client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("track_1", nil).AnyTimes()

	// Done/Abort
	subscriber.EXPECT().UnsubscribeTicker("BTC_USDT").Return(nil).AnyTimes()

	cfg := config.SymbolConfig{
		Symbol:         "BTC_USDT",
		MinFundingRate: 0.001,
		FundingTrap:    domain.FundingTrapConfig{Enabled: true},
	}
	global := &config.Config{System: &config.SystemConfig{Safety: config.SafetyConfig{
		BufferTime:        10 * 1000000,
		PostSettleTimeout: 10 * 1000000000, // 10s
	}}}

	deps := application.Deps{
		Client:        client,
		WsSub:         subscriber,
		OrderNotifier: ws,
		TickerStore:   tickerStore,
		ContractStore: contractStore,
		PriceStore:    priceStore,
		FundingStore:  fundingStore,
		KlineStore:    klineStore,
		DepthStore:    depthStore,
		Clock:         clock,
		Log:           slog.Default(),
	}

	o := application.NewCycleOrchestrator(cfg, global, deps)

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	// Simulate settle time right now.
	o.Run(ctx, time.Now())
}
