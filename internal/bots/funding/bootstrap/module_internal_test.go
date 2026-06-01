package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

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
	bus := eventbus.New(bootstrapTestLogger())
	t.Cleanup(func() { _ = bus.Close() })
	engine := &infraapp.Engine{
		Bus: bus,
	}

	global := &fundingconfig.Config{}
	mockNotifier := mocks.NewMockNotifier(ctrl)

	assert.Equal(t, "reversion", provideReversionStrategy(engine, global, mockNotifier, bootstrapTestLogger()).Flow())
	assert.Equal(t, "trap", provideTrapStrategy(engine, global, bootstrapTestLogger()).Flow())
	assert.Equal(t, "trailing", provideTrailingStrategy(engine, global, bootstrapTestLogger()).Flow())
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

	httpClient := provideHTTPClient(bootstrapTestLogger())
	require.NotNil(t, httpClient)
	assert.IsType(t, &http.Client{}, httpClient)

	engine := &infraapp.Engine{
		Bus:       eventbus.New(bootstrapTestLogger()),
		Providers: map[string]*infraapp.ExchangeProvider{},
	}
	t.Cleanup(func() { _ = engine.Bus.Close() })

	revStrat := provideReversionStrategy(engine, &fundingconfig.Config{}, n, bootstrapTestLogger())
	trapStrat := provideTrapStrategy(engine, &fundingconfig.Config{}, bootstrapTestLogger())
	trailStrat := provideTrailingStrategy(engine, &fundingconfig.Config{}, bootstrapTestLogger())

	bot := provideBot(
		&fundingconfig.Config{},
		&fundingconfig.SystemConfig{},
		engine,
		n,
		revStrat,
		trapStrat,
		trailStrat,
		bootstrapTestLogger(),
	)
	require.NotNil(t, bot)
}
