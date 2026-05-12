package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"crypto-bot/pkg/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testConfig struct {
	Name  string `json:"name"`
	Value int    `json:"value"`
}

func TestLoad_ValidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"name":"test","value":42}`), 0o600))

	cfg, err := config.Load[testConfig](path)
	require.NoError(t, err)
	assert.Equal(t, "test", cfg.Name)
	assert.Equal(t, 42, cfg.Value)
}

func TestLoad_JSONC(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.jsonc")
	data := `{
		// This is a comment
		"name": "jsonc",
		"value": 99
	}`
	require.NoError(t, os.WriteFile(path, []byte(data), 0o600))

	cfg, err := config.Load[testConfig](path)
	require.NoError(t, err)
	assert.Equal(t, "jsonc", cfg.Name)
	assert.Equal(t, 99, cfg.Value)
}

func TestLoad_FileNotFound(t *testing.T) {
	t.Parallel()

	_, err := config.Load[testConfig]("/nonexistent/file.json")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read config")
}

func TestLoad_InvalidJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	require.NoError(t, os.WriteFile(path, []byte(`{not valid`), 0o600))

	_, err := config.Load[testConfig](path)
	assert.Error(t, err)
}

func TestLoad_EmptyJSON(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o600))

	cfg, err := config.Load[testConfig](path)
	require.NoError(t, err)
	assert.Equal(t, "", cfg.Name)
	assert.Equal(t, 0, cfg.Value)
}
