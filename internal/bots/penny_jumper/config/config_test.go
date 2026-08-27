package config_test

import (
	"path/filepath"
	"testing"
	"time"

	pjconfig "crypto-bot/internal/bots/penny_jumper/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPennyJumperConfig_Local(t *testing.T) {
	t.Setenv("AI_PROXY_URL", "http://127.0.0.1:8317")
	t.Setenv("AI_PROXY_API_KEY", "sk-local-proxy-secret")
	t.Setenv("AI_PROXY_MODEL", "gemini-3.7-flash-high")

	botPath := filepath.Join("..", "..", "..", "..", "configs", "penny_jumper", "local", "penny_jumper.jsonc")
	blacklistPath := filepath.Join("..", "..", "..", "..", "configs", "penny_jumper", "local", "blacklist.jsonc")

	cfg, blacklist, err := pjconfig.LoadPennyJumperConfig(botPath, blacklistPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Contains(t, []string{"local", "model", "dual"}, cfg.WallJudge.Mode)
	assert.Equal(t, "http://127.0.0.1:8317", cfg.WallJudge.Endpoint)
	assert.Equal(t, "sk-local-proxy-secret", cfg.WallJudge.ApiKey)
	assert.Equal(t, "gemini-3.7-flash-high", cfg.WallJudge.ModelName)
	assert.Equal(t, 5*time.Second, cfg.WallJudge.EvalCooldown.Duration())
	assert.NotNil(t, blacklist)
}

func TestLoadPennyJumperConfig_MissingAIProxyCredentials(t *testing.T) {
	t.Setenv("AI_PROXY_URL", "")
	t.Setenv("AI_PROXY_API_KEY", "")
	t.Setenv("AI_PROXY_MODEL", "")
	t.Setenv("BITWARDEN_ACCESS_TOKEN", "")

	botPath := filepath.Join("..", "..", "..", "..", "configs", "penny_jumper", "local", "penny_jumper.jsonc")
	blacklistPath := filepath.Join("..", "..", "..", "..", "configs", "penny_jumper", "local", "blacklist.jsonc")

	cfg, _, err := pjconfig.LoadPennyJumperConfig(botPath, blacklistPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "AI_PROXY_URL is required")
}
