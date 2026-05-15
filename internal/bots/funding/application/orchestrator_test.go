package application_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"

	"go.uber.org/mock/gomock"
)

func TestCycleOrchestrator_HappyPath(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	o, m := setupOrchestrator(t, ctrl)

	m.tickerStore.EXPECT().GetTicker(gomock.Any(), "BTC_USDT").Return(&store.TickerData{
		Symbol:      "BTC_USDT",
		FundingRate: 0.005,
		LastPrice:   50000,
		BestBid:     49999,
		BestAsk:     50001,
	}, nil).AnyTimes()
	m.contractStore.EXPECT().GetContract(gomock.Any(), "BTC_USDT").Return(&store.ContractData{
		PriceUnit:    0.1,
		VolUnit:      1,
		MinVol:       1,
		ContractSize: 0.001,
		TakerFeeRate: 0.0006,
	}, nil).AnyTimes()
	m.subscriber.EXPECT().SubscribeTicker(gomock.Any(), "BTC_USDT").Return(nil).AnyTimes()
	m.priceStore.EXPECT().GetPrice(gomock.Any(), "BTC_USDT", gomock.Any()).Return(&store.PriceData{
		BestBid: 49999, BestAsk: 50001, LastPrice: 50000,
	}, nil).AnyTimes()
	m.clock.EXPECT().Until(gomock.Any()).Return(time.Duration(0)).AnyTimes()
	m.clock.EXPECT().LatencyMs().Return(int64(50)).AnyTimes()
	m.clock.EXPECT().Now().Return(time.Now()).AnyTimes()
	m.clock.EXPECT().GetServerTime().Return(time.Now().UnixMilli()).AnyTimes()
	m.clock.EXPECT().Offset().Return(int64(0)).AnyTimes()

	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("ioc_1", nil).AnyTimes()
	m.ws.EXPECT().OnOrderUpdate(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, orderID string, duration time.Duration, callback func(exchange.WsOrderDeal)) {
		go func() {
			time.Sleep(10 * time.Millisecond)
			callback(exchange.WsOrderDeal{
				OrderID:      orderID,
				State:        exchange.OrderStateFilled,
				DealVol:      1.0,
				DealAvgPrice: 50000,
			})
		}()
	}).AnyTimes()

	m.ws.EXPECT().RemoveOrderCallback(gomock.Any()).AnyTimes()
	m.client.EXPECT().CreateOrder(gomock.Any(), gomock.Any()).Return("trap_1", nil).AnyTimes()
	m.client.EXPECT().CreateTrackOrder(gomock.Any(), gomock.Any()).Return("track_1", nil).AnyTimes()

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	o.Run(ctx, time.Now())
}
