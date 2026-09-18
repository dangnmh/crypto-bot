package reversion

import (
	"context"
	"errors"
	"testing"

	"crypto-bot/internal/bots/funding/application/strategy"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/testutil/mocks"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
)

type mockDepthProviderClient struct {
	*mocks.MockClient
	depthOB  *shared.OrderBook
	depthErr error
}

func (m *mockDepthProviderClient) GetDepth(ctx context.Context, symbol string) (*shared.OrderBook, error) {
	return m.depthOB, m.depthErr
}

func TestCheckDepthImbalance_Evaluation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		bidVol      float64
		askVol      float64
		assertRatio func(t *testing.T, ratio float64)
	}{
		{
			name:   "heavy bids triggers high ratio",
			bidVol: 300.0,
			askVol: 100.0,
			assertRatio: func(t *testing.T, ratio float64) {
				assert.GreaterOrEqual(t, ratio, 1.5)
			},
		},
		{
			name:   "favorable asks triggers low ratio",
			bidVol: 50.0,
			askVol: 200.0,
			assertRatio: func(t *testing.T, ratio float64) {
				assert.Less(t, ratio, 1.0)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctrl := gomock.NewController(t)
			defer ctrl.Finish()

			mockDepthStore := mocks.NewMockDepthReader(ctrl)
			ob := &shared.OrderBook{
				Symbol: "BTC_USDT",
				Bids:   []shared.OrderBookEntry{{Price: 100.0, Volume: tt.bidVol}},
				Asks:   []shared.OrderBookEntry{{Price: 100.1, Volume: tt.askVol}},
			}
			mockDepthStore.EXPECT().GetDepth(gomock.Any(), "BTC_USDT").Return(ob, nil)

			runner := &StatelessRunner{
				deps: strategy.Deps{DepthStore: mockDepthStore},
				log:  reversionTestLogger(),
			}

			cand := &fundingdomain.Candidate{
				Symbol: "BTC_USDT",
				Side:   shared.SideOpenShort,
			}

			runner.checkDepthImbalance(context.Background(), cand)
			tt.assertRatio(t, cand.ImbalanceRatio)
		})
	}
}

func TestFetchOrderBook_DepthStoreFallbackToREST(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDepthStore := mocks.NewMockDepthReader(ctrl)
	mockDepthStore.EXPECT().GetDepth(gomock.Any(), "BTC_USDT").Return(nil, errors.New("not found"))

	baseClient := mocks.NewMockClient(ctrl)
	restOB := &shared.OrderBook{
		Symbol: "BTC_USDT",
		Bids:   []shared.OrderBookEntry{{Price: 100, Volume: 10}},
		Asks:   []shared.OrderBookEntry{{Price: 101, Volume: 10}},
	}
	dpClient := &mockDepthProviderClient{
		MockClient: baseClient,
		depthOB:    restOB,
	}

	runner := &StatelessRunner{
		deps: strategy.Deps{
			DepthStore: mockDepthStore,
			Client:     dpClient,
		},
		log: reversionTestLogger(),
	}

	ob, err := runner.fetchOrderBook(context.Background(), "BTC_USDT")
	require.NoError(t, err)
	assert.Equal(t, restOB, ob)
}

func TestFetchOrderBook_NoProviderOrError(t *testing.T) {
	t.Parallel()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	baseClient := mocks.NewMockClient(ctrl)
	runner := &StatelessRunner{
		deps: strategy.Deps{
			Client: baseClient, // does not implement DepthProvider
		},
		log: reversionTestLogger(),
	}

	ob, err := runner.fetchOrderBook(context.Background(), "BTC_USDT")
	assert.Error(t, err)
	assert.Nil(t, ob)

	// Safe checkDepthImbalance with nil candidate
	runner.checkDepthImbalance(context.Background(), nil)

	// Safe checkDepthImbalance when fetch fails
	cand := &fundingdomain.Candidate{
		Symbol: "BTC_USDT",
	}
	runner.checkDepthImbalance(context.Background(), cand)
	assert.Zero(t, cand.ImbalanceRatio)
}
