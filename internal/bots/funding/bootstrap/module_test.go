package bootstrap_test

import (
	"testing"

	"crypto-bot/internal/bots/funding/bootstrap"

	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
)

func TestModuleDependencyGraph(t *testing.T) {
	t.Parallel()

	err := fx.ValidateApp(bootstrap.Module(bootstrap.ConfigPaths{
		System: "system.jsonc",
		Bot:    "funding.jsonc",
	}))
	require.NoError(t, err)
}
