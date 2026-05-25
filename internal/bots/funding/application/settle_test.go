package application_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/testutil/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestGetNextSettleTime(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(2 * time.Minute).UTC().Truncate(time.Second)
	got, err := application.GetNextSettleTime(context.Background(), future.Format(time.RFC3339), "BTC_USDT", nil)
	require.NoError(t, err)
	assert.Equal(t, future, got)

	_, err = application.GetNextSettleTime(context.Background(), "bad", "BTC_USDT", nil)
	require.Error(t, err)
}

func TestGetNextSettleTimeUsesFundingStore(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	funding := mocks.NewMockFundingReader(ctrl)
	want := time.Now().Add(time.Hour)
	funding.EXPECT().GetSettleTime(gomock.Any(), "BTC_USDT").Return(want, nil)

	got, err := application.GetNextSettleTime(context.Background(), "", "BTC_USDT", funding)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestGetNextSettleTimeStoreError(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	funding := mocks.NewMockFundingReader(ctrl)
	funding.EXPECT().GetSettleTime(gomock.Any(), "BTC_USDT").Return(time.Time{}, errors.New("missing"))

	_, err := application.GetNextSettleTime(context.Background(), time.Now().Format(time.RFC3339), "BTC_USDT", funding)
	require.Error(t, err)
}

var _ store.FundingReader = (*mocks.MockFundingReader)(nil)
