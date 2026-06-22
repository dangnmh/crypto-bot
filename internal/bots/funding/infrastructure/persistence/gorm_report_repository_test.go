package persistence_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/domain"
	persistence "crypto-bot/internal/bots/funding/infrastructure/persistence"
	shared "crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
)

func TestToGormModel(t *testing.T) {
	t.Parallel()

	now := time.Now()
	report := &domain.TradeReport{
		ReqID:               "req_test_123",
		EventID:             "evt_test_456",
		Timestamp:           now,
		SettleTime:          now.Add(time.Hour),
		Exchange:            "bybit",
		Symbol:              "BTC-USDT-SWAP",
		NormalizedSymbol:    "BTC",
		Side:                shared.SideOpenLong,
		FundingRate:         0.0015,
		CandidateFoundTime:  now.Add(-time.Minute),
		MarginUSDT:          100.0,
		Leverage:            10,
		BufferTimeMs:        500,
		LatencyRTTMs:        80,
		ActualSlippage:      0.05,
		FireOffsetMs:        120,
		IOCOrderID:          "ioc_order_789",
		IOCOutcome:          "filled",
		IOCReason:           "success",
		FireIOCTime:         now.Add(time.Second),
		LocalFireIOCTime:    now.Add(2 * time.Second),
		OrderFilled:         true,
		FillPrice:           60000.0,
		ClosePrice:          60100.0,
		VolumeUSDT:          1000.0,
		GrossProfit:         10.0,
		NetProfit:           9.5,
		PnLPct:              1.0,
		Fee:                 0.5,
		HoldFee:             0.0,
		HoldDurationMs:      60000,
		ExitReason:          "tp",
		CloseRetryCount:     0,
		ForceCloseAttempted: false,
		ForceCloseSucceeded: false,
		Status:              "completed",
		ErrorMsg:            "",
	}

	model := persistence.ToGormModel(report)

	assert.Equal(t, "req_test_123", model.ReqID)
	assert.Equal(t, "evt_test_456", model.EventID)
	assert.Equal(t, now, model.Timestamp)
	assert.Equal(t, now.Add(time.Hour), model.SettleTime)
	assert.Equal(t, "bybit", model.Exchange)
	assert.Equal(t, "BTC-USDT-SWAP", model.Symbol)
	assert.Equal(t, "BTC", model.NormalizedSymbol)
	assert.Equal(t, "LONG", model.Side)
	assert.Equal(t, 0.0015, model.FundingRate)
	assert.Equal(t, now.Add(-time.Minute), model.CandidateFoundTime)
	assert.Equal(t, 100.0, model.MarginUSDT)
	assert.Equal(t, 10, model.Leverage)
	assert.Equal(t, int64(500), model.BufferTimeMs)
	assert.Equal(t, int64(80), model.LatencyRTTMs)
	assert.Equal(t, 0.05, model.ActualSlippage)
	assert.Equal(t, int64(120), model.FireOffsetMs)
	assert.Equal(t, "ioc_order_789", model.IOCOrderID)
	assert.Equal(t, "filled", model.IOCOutcome)
	assert.Equal(t, "success", model.IOCReason)
	assert.Equal(t, now.Add(time.Second), model.FireIOCTime)
	assert.Equal(t, now.Add(2*time.Second), model.LocalFireIOCTime)
	assert.True(t, model.OrderFilled)
	assert.Equal(t, 60000.0, model.FillPrice)
	assert.Equal(t, 60100.0, model.ClosePrice)
	assert.Equal(t, 1000.0, model.VolumeUSDT)
	assert.Equal(t, 10.0, model.GrossProfit)
	assert.Equal(t, 9.5, model.NetProfit)
	assert.Equal(t, 1.0, model.PnLPct)
	assert.Equal(t, 0.5, model.Fee)
	assert.Equal(t, 0.0, model.HoldFee)
	assert.Equal(t, int64(60000), model.HoldDurationMs)
	assert.Equal(t, "tp", model.ExitReason)
	assert.Equal(t, 0, model.CloseRetryCount)
	assert.False(t, model.ForceCloseAttempted)
	assert.False(t, model.ForceCloseSucceeded)
	assert.Equal(t, "completed", model.Status)
	assert.Equal(t, "", model.ErrorMsg)
}

func TestGormTradeReportRepository_Save_NilDB(t *testing.T) {
	t.Parallel()

	repo := persistence.NewGormTradeReportRepository(nil)

	report := &domain.TradeReport{
		ReqID:      "req_dry_run_1",
		EventID:    "evt_dry_run_1",
		Timestamp:  time.Now(),
		Exchange:   "bybit",
		Symbol:     "BTC-USDT-SWAP",
		Side:       shared.SideOpenShort,
		Status:     "aborted",
		MarginUSDT: 50.0,
		Leverage:   5,
	}

	err := repo.Save(context.Background(), report)
	assert.NoError(t, err)
}

func TestTableName(t *testing.T) {
	t.Parallel()

	report := persistence.ReversionTradeReport{}
	assert.Equal(t, "reversion_trade_reports", report.TableName())
}
