package bootstrap

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"testing"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/reversion"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/config"
	exchange "crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/pkg/eventbus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx/fxtest"
)

func bootstrapTestLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestProviderFactoriesReturnStrategies(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(bootstrapTestLogger())
	t.Cleanup(func() { _ = bus.Close() })
	engine := &infraapp.Engine{
		Bus: bus,
	}

	global := &fundingconfig.Config{}

	assert.Equal(t, reversion.FlowIDFundingReversion, reversion.ProvideReversionStrategy(engine, global, nil, nil, bootstrapTestLogger()).Flow())
}

func TestProvideLoggerNotifierHTTPAndBot(t *testing.T) {
	t.Parallel()

	lc := fxtest.NewLifecycle(t)
	log := provideLogger(lc, &fundingconfig.SystemConfig{})
	require.NotNil(t, log)

	notiCfg := provideNotifierConfig(&fundingconfig.SystemConfig{
		NotiConfig: config.NotiConfig{
			Enabled:                false,
			TelegramChatID:         "123",
			TelegramCriticalChatID: "456",
			TelegramBotToken:       "token",
		},
	}, &fundingconfig.Config{})
	assert.Equal(t, "123", notiCfg.TelegramChatID)
	assert.Equal(t, "456", notiCfg.TelegramCriticalChatID)
	assert.Equal(t, "token", notiCfg.TelegramBotToken)
	n, err := notifier.ProvideNotifier(lc, notiCfg, bootstrapTestLogger())
	require.NoError(t, err)
	require.NotNil(t, n)
	require.NoError(t, lc.Start(context.Background()))
	require.NoError(t, lc.Stop(context.Background()))

	httpClient := exchange.ProvideHTTPClient(bootstrapTestLogger())
	require.NotNil(t, httpClient)
	assert.IsType(t, &http.Client{}, httpClient)

	engine := &infraapp.Engine{
		Bus:       eventbus.New(bootstrapTestLogger()),
		Providers: map[string]*infraapp.ExchangeProvider{},
	}
	t.Cleanup(func() { _ = engine.Bus.Close() })

	revStrat := reversion.ProvideReversionStrategy(engine, &fundingconfig.Config{}, nil, nil, bootstrapTestLogger())

	bot := application.ProvideFundingBot(
		&fundingconfig.Config{},
		&fundingconfig.SystemConfig{},
		engine,
		n,
		revStrat,
		nil,
		nil,
		nil,
		nil,
		nil,
		bootstrapTestLogger(),
	)
	require.NotNil(t, bot)
}
