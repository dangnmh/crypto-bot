package store_test

import (
	"context"
	"sync"
	"testing"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/testutil/mocks"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

func TestContractStore_GetContract(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)

	client := mocks.NewMockClient(ctrl)
	client.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
		{Symbol: "BTC_USDT", PriceUnit: 0.1, ContractSize: 0.001},
	}, nil).AnyTimes()

	wg := &sync.WaitGroup{}
	cs := store.NewContractStore(wg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	go cs.StartContractSync(ctx, client, time.Hour)
	wg.Wait()

	cd, err := cs.GetContract(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, 0.1, cd.PriceUnit)
	assert.Equal(t, 0.001, cd.ContractSize)
}

func TestContractStore_GetContract_Missing(t *testing.T) {
	t.Parallel()

	wg := &sync.WaitGroup{}
	cs := store.NewContractStore(wg)
	wg.Done()

	_, err := cs.GetContract(context.Background(), "NONEXISTENT")
	assert.Error(t, err)
}
