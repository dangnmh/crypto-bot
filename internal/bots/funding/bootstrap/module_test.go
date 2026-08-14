package bootstrap_test

import (
	"context"
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
)

func TestModuleDependencyGraph(t *testing.T) {
	t.Parallel()

	err := fx.ValidateApp(bootstrap.Module(bootstrap.ConfigPaths{
		System:    "system.jsonc",
		Exchange:  "exchange.jsonc",
		Bot:       "funding.jsonc",
		Blacklist: "blacklist.jsonc",
		Reversion: "reversion.jsonc",
	}))
	require.NoError(t, err)
}

func TestModuleProvidesRuntimeDependencies(t *testing.T) {
	t.Setenv("MEXC_API_KEY", "test-key")
	t.Setenv("MEXC_API_SECRET", "test-secret")

	dir := t.TempDir()
	systemPath := filepath.Join(dir, "system.jsonc")
	exchangePath := filepath.Join(dir, "exchange.jsonc")
	fundingPath := filepath.Join(dir, "funding.jsonc")
	require.NoError(t, os.WriteFile(systemPath, []byte(`{
		"dryRun": true,
		"notifier": {"enabled": false}
	}`), 0o600))
	require.NoError(t, os.WriteFile(exchangePath, []byte(`{
		"exchange": {
			"mexc": {
				"enable": true,
				"future": {
					"enable": true,
					"baseURL": "https://example.test",
					"websocket": {"wsURL": "wss://example.test/ws", "maxPairsPerWSConn": 2}
				}
			}
		}
	}`), 0o600))
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[
		{"symbol": "BTC_USDT", "exchange": "mexc_futures", "marginUSDT": 10, "leverage": 5}
	]`), 0o600))
	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	reversionPath := filepath.Join(dir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(reversionPath, []byte(`{"enabled": true, "scanners": {"configured": true}}`), 0o600))

	var (
		log        *slog.Logger
		systemCfg  *fundingconfig.SystemConfig
		fundingCfg *fundingconfig.Config
		httpClient *http.Client
		engine     *infraapp.Engine
		bot        infraapp.Bot
		n          notifier.Notifier
	)

	app := fx.New(
		bootstrap.Module(bootstrap.ConfigPaths{
			System:    systemPath,
			Exchange:  exchangePath,
			Bot:       fundingPath,
			Blacklist: blacklistPath,
			Reversion: reversionPath,
		}),
		fx.Populate(&log, &systemCfg, &fundingCfg, &httpClient, &engine, &bot, &n),
		fx.NopLogger,
	)
	require.NoError(t, app.Err())

	require.NotNil(t, log)
	require.NotNil(t, systemCfg)
	require.NotNil(t, fundingCfg)
	require.NotNil(t, httpClient)
	require.NotNil(t, engine)
	t.Cleanup(func() { _ = engine.Shutdown(context.Background()) })
	require.NotNil(t, bot)
	require.NotNil(t, n)
	require.True(t, systemCfg.DryRun)
	require.Len(t, fundingCfg.Symbols, 1)
}
