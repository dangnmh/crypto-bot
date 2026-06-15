package bootstrap_test

import (
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"crypto-bot/internal/bots/funding/bootstrap"
	fundingconfig "crypto-bot/internal/bots/funding/config"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/notifier"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestModuleDependencyGraph(t *testing.T) {
	t.Parallel()

	err := fx.ValidateApp(bootstrap.Module(bootstrap.ConfigPaths{
		System: "system.jsonc",
		Bot:    "funding.jsonc",
	}))
	require.NoError(t, err)
}

func TestModuleProvidesRuntimeDependencies(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.jsonc")
	fundingPath := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(systemPath, []byte(`{
		"dryRun": true,
		"sync": {},
		"safety": {},
		"exchange": {
			"mexc": {
				"enable": true,
				"future": {"baseURL": "https://example.test"},
				"websocket": {"wsURL": "wss://example.test/ws", "maxPairsPerWSConn": 2}
			}
		},
		"notifier": {"enabled": false}
	}`), 0o600))
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[
		{"symbol": "BTC_USDT", "exchange": "mexc", "marginUSDT": 10, "leverage": 5}
	]`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "reversion.jsonc"), []byte(`{"enabled": true}`), 0o600))

	var (
		log        *slog.Logger
		systemCfg  *fundingconfig.SystemConfig
		fundingCfg *fundingconfig.Config
		httpClient *http.Client
		engine     *infraapp.Engine
		bot        infraapp.Bot
		n          notifier.Notifier
	)

	app := fxtest.New(
		t,
		bootstrap.Module(bootstrap.ConfigPaths{System: systemPath, Bot: fundingPath}),
		fx.Populate(&log, &systemCfg, &fundingCfg, &httpClient, &engine, &bot, &n),
	)
	require.NotNil(t, app)

	require.NotNil(t, log)
	require.NotNil(t, systemCfg)
	require.NotNil(t, fundingCfg)
	require.NotNil(t, httpClient)
	require.NotNil(t, engine)
	require.NotNil(t, bot)
	require.NotNil(t, n)
	require.True(t, systemCfg.DryRun)
	require.Len(t, fundingCfg.Symbols, 1)
}
