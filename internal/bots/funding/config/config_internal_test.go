package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAccountConfigPath(t *testing.T) {
	t.Parallel()

	// 1. Empty or whitespace
	assert.Equal(t, "", resolveAccountConfigPath("/base", ""))
	assert.Equal(t, "", resolveAccountConfigPath("/base", "   "))

	// 2. Absolute path should be returned trimmed
	assert.Equal(t, "/abs/path/to/config.jsonc", resolveAccountConfigPath("/base", "/abs/path/to/config.jsonc"))

	// 3. Relative to baseDir
	tmpDir := t.TempDir()
	accountsSubDir := filepath.Join(tmpDir, "accounts", "mexc_main")
	require.NoError(t, os.MkdirAll(accountsSubDir, 0o755))
	targetFile := filepath.Join(accountsSubDir, "reversion.jsonc")
	require.NoError(t, os.WriteFile(targetFile, []byte("{}"), 0o600))

	// Path relative from tmpDir (baseDir = tmpDir, rawPath = "accounts/mexc_main/reversion.jsonc")
	resolved := resolveAccountConfigPath(tmpDir, "accounts/mexc_main/reversion.jsonc")
	assert.Equal(t, targetFile, resolved)

	// Path with ./ prefix relative from tmpDir
	resolvedWithDot := resolveAccountConfigPath(tmpDir, "./accounts/mexc_main/reversion.jsonc")
	assert.Equal(t, targetFile, resolvedWithDot)

	// 4. Flattened filenames for Kubernetes ConfigMap (dots or underscores)
	flatDotDir := t.TempDir()
	flatDotFile := filepath.Join(flatDotDir, "accounts.mexc_main.reversion.jsonc")
	require.NoError(t, os.WriteFile(flatDotFile, []byte("{}"), 0o600))
	assert.Equal(t, flatDotFile, resolveAccountConfigPath(flatDotDir, "accounts/mexc_main/reversion.jsonc"))
	assert.Equal(t, flatDotFile, resolveAccountConfigPath(flatDotDir, "./accounts/mexc_main/reversion.jsonc"))

	flatUnderDir := t.TempDir()
	flatUnderFile := filepath.Join(flatUnderDir, "accounts_mexc_main_reversion.jsonc")
	require.NoError(t, os.WriteFile(flatUnderFile, []byte("{}"), 0o600))
	assert.Equal(t, flatUnderFile, resolveAccountConfigPath(flatUnderDir, "accounts/mexc_main/reversion.jsonc"))

	// 5. Non-existent file returns path joined with baseDir
	assert.Equal(t, filepath.Join(tmpDir, "nonexistent.jsonc"), resolveAccountConfigPath(tmpDir, "nonexistent.jsonc"))
}
