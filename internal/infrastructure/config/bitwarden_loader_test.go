//nolint:testpackage // These tests exercise unexported Bitwarden loader cache paths.
package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewBitwardenLoaderRequiresEnvironment(t *testing.T) {
	t.Setenv("BITWARDEN_ACCESS_TOKEN", "")
	t.Setenv("BITWARDEN_ORGANIZATION_ID", "")
	t.Setenv("BITWARDEN_PROJECT_NAME", "")

	_, err := NewBitwardenLoader()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BITWARDEN_ACCESS_TOKEN")

	t.Setenv("BITWARDEN_ACCESS_TOKEN", "token")
	_, err = NewBitwardenLoader()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BITWARDEN_ORGANIZATION_ID")

	t.Setenv("BITWARDEN_ORGANIZATION_ID", "org")
	_, err = NewBitwardenLoader()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "BITWARDEN_PROJECT_NAME")
}

func TestBitwardenLoaderUsesCachedProjectAndSecret(t *testing.T) {
	t.Parallel()

	loader := &BitwardenLoader{
		projectID:   "project-1",
		projectName: "prod",
		secretCache: map[string]string{
			"MEXC_API_KEY": "key",
		},
	}

	projectID, err := loader.resolveProjectID()
	require.NoError(t, err)
	assert.Equal(t, "project-1", projectID)

	secret, err := loader.GetSecret("MEXC_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "key", secret)
}
