package bootstrap_test

import (
	"context"
	"fmt"
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
		Accounts:  "accounts.jsonc",
		System:    "system.jsonc",
		Exchange:  "exchange.jsonc",
		Blacklist: "blacklist.jsonc",
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
		"mexc_futures": {
			"enable": true,
			"accountType": "futures",
			"baseURL": "https://example.test",
			"websocket": {"publicURL": "wss://example.test/ws", "privateURL": "wss://example.test/ws", "maxPairsPerWSConn": 2}
		}
	}`), 0o600))
	require.NoError(t, os.WriteFile(fundingPath, []byte(`[
		{"symbol": "BTC_USDT", "exchange": "mexc_futures", "marginUSDT": 10, "leverage": 5}
	]`), 0o600))
	blacklistPath := filepath.Join(dir, "blacklist.jsonc")
	reversionPath := filepath.Join(dir, "reversion.jsonc")
	obfuscatorPath := filepath.Join(dir, "obfuscator.jsonc")
	dilutionPath := filepath.Join(dir, "dilution.jsonc")
	accountsPath := filepath.Join(dir, "accounts.jsonc")
	require.NoError(t, os.WriteFile(blacklistPath, []byte("{}"), 0o600))
	require.NoError(t, os.WriteFile(reversionPath, []byte(`{"enabled": true}`), 0o600))
	require.NoError(t, os.WriteFile(obfuscatorPath, []byte(`{"enabled": false, "pollInterval": "1m", "lookbackWindow": "24h"}`), 0o600))
	require.NoError(t, os.WriteFile(dilutionPath, []byte(`{"enabled": false, "pollInterval": "5s"}`), 0o600))

	accountsContent := fmt.Sprintf(`{
		"accounts": [
			{
				"id": "mexc_main",
				"exchange": "mexc_futures",
				"enabled": true,
				"scanners": {
					"configured": true,
					"schedule": true
				},
				"env": {
					"apiKey": "MEXC_API_KEY",
					"apiSecret": "MEXC_API_SECRET"
				},
				"configs": {
					"reversion": %q,
					"obfuscator": %q,
					"dilution": %q,
					"funding": %q
				}
			}
		]
	}`, reversionPath, obfuscatorPath, dilutionPath, fundingPath)
	require.NoError(t, os.WriteFile(accountsPath, []byte(accountsContent), 0o600))

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
			Accounts:  accountsPath,
			System:    systemPath,
			Exchange:  exchangePath,
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
	require.Len(t, fundingCfg.AllSymbols(), 1)
}
