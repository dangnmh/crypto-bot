package bootstrap_test

import (
	"testing"

	"crypto-bot/internal/bots/penny_jumper/bootstrap"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

func TestBootstrap_ModuleValidation(t *testing.T) {
	t.Parallel()

	paths := bootstrap.ConfigPaths{
		System:    "../../../../configs/penny_jumper/local/system.jsonc",
		Exchange:  "../../../../configs/penny_jumper/local/exchange.jsonc",
		Bot:       "../../../../configs/penny_jumper/local/penny_jumper.jsonc",
		Blacklist: "../../../../configs/penny_jumper/local/blacklist.jsonc",
	}

	err := fx.ValidateApp(
		bootstrap.Module(paths),
	)
	require.NoError(t, err, "FX module graph validation failed")
}
