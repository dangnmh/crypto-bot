package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"crypto-bot/internal/bots/funding/application"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/config"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/eventbus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
	"go.uber.org/mock/gomock"
)

func bootstrapTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestProviderFactoriesReturnStrategies(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	deps := application.Deps{
		Client:   mocks.NewMockClient(ctrl),
		Log:      bootstrapTestLogger(),
		EventBus: eventbus.New(bootstrapTestLogger()),
	}
	t.Cleanup(func() { _ = deps.EventBus.Close() })

	global := &fundingconfig.Config{}
	sym := fundingconfig.SymbolConfig{Symbol: "BTC_USDT"}

	assert.Equal(t, "reversion", provideReversionStrategyFactory()(sym, global, deps).Flow())
	assert.Equal(t, "trap", provideTrapStrategyFactory()(sym, global, deps).Flow())
	assert.Equal(t, "trailing", provideTrailingStrategyFactory()(sym, global, deps).Flow())
}

func TestProvideLoggerNotifierHTTPAndBot(t *testing.T) {
	t.Parallel()

	lc := fxtest.NewLifecycle(t)
	log := provideLogger(lc, &fundingconfig.SystemConfig{})
	require.NotNil(t, log)

	n, err := provideNotifier(lc, &fundingconfig.SystemConfig{
		SystemConfig: config.SystemConfig{NotiConfig: config.NotiConfig{Enabled: false}},
	}, bootstrapTestLogger())
	require.NoError(t, err)
	require.NotNil(t, n)
	require.NoError(t, lc.Start(context.Background()))
	require.NoError(t, lc.Stop(context.Background()))

	httpClient := provideHTTPClient()
	require.NotNil(t, httpClient)
	assert.IsType(t, &http.Client{}, httpClient)

	engine := &infraapp.Engine{
		Bus:       eventbus.New(bootstrapTestLogger()),
		Providers: map[string]*infraapp.ExchangeProvider{},
	}
	t.Cleanup(func() { _ = engine.Bus.Close() })
	bot := provideBot(
		&fundingconfig.Config{},
		&fundingconfig.SystemConfig{},
		engine,
		n,
		provideReversionStrategyFactory(),
		provideTrapStrategyFactory(),
		provideTrailingStrategyFactory(),
		bootstrapTestLogger(),
	)
	require.NotNil(t, bot)
}
