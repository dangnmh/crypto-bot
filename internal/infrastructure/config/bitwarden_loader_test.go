//nolint:testpackage // These tests exercise unexported Bitwarden loader cache paths.
package config

import (
	"errors"
	"testing"
	"time"

	sdk "github.com/bitwarden/sdk-go/v2"
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

func TestBitwardenLoaderResolvesProjectAndSecret(t *testing.T) {
	t.Parallel()

	projectID := "project-1"
	client := &fakeBitwardenClient{
		projects: &fakeProjects{
			list: &sdk.ProjectsResponse{Data: []sdk.ProjectResponse{
				{ID: "other", Name: "dev"},
				{ID: projectID, Name: "prod"},
			}},
		},
		secrets: &fakeSecrets{
			sync: &sdk.SecretsSyncResponse{Secrets: []sdk.SecretResponse{
				{Key: "MEXC_API_KEY", Value: "wrong", ProjectID: ptr("other")},
				{Key: "MEXC_API_KEY", Value: "key", ProjectID: &projectID},
			}},
		},
	}
	loader := &BitwardenLoader{
		client:         client,
		organizationID: "org",
		projectName:    "prod",
		secretCache:    make(map[string]string),
	}

	gotProjectID, err := loader.resolveProjectID()
	require.NoError(t, err)
	assert.Equal(t, projectID, gotProjectID)

	secret, err := loader.GetSecret("MEXC_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "key", secret)
	assert.Equal(t, "key", loader.secretCache["MEXC_API_KEY"])
}

func TestBitwardenLoaderErrorPaths(t *testing.T) {
	t.Parallel()

	t.Run("list projects error", func(t *testing.T) {
		t.Parallel()
		loader := &BitwardenLoader{
			client:      &fakeBitwardenClient{projects: &fakeProjects{err: errors.New("list failed")}},
			projectName: "prod",
			secretCache: make(map[string]string),
		}

		_, err := loader.resolveProjectID()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "list failed")
	})

	t.Run("project missing", func(t *testing.T) {
		t.Parallel()
		loader := &BitwardenLoader{
			client:      &fakeBitwardenClient{projects: &fakeProjects{list: &sdk.ProjectsResponse{}}},
			projectName: "prod",
			secretCache: make(map[string]string),
		}

		_, err := loader.resolveProjectID()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "project 'prod' not found")
	})

	t.Run("sync secrets error", func(t *testing.T) {
		t.Parallel()
		loader := &BitwardenLoader{
			client: &fakeBitwardenClient{
				secrets: &fakeSecrets{err: errors.New("sync failed")},
			},
			projectID:   "project-1",
			projectName: "prod",
			secretCache: make(map[string]string),
		}

		_, err := loader.GetSecret("MEXC_API_KEY")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sync failed")
	})

	t.Run("secret missing", func(t *testing.T) {
		t.Parallel()
		loader := &BitwardenLoader{
			client: &fakeBitwardenClient{
				secrets: &fakeSecrets{sync: &sdk.SecretsSyncResponse{Secrets: []sdk.SecretResponse{
					{Key: "OTHER", Value: "x", ProjectID: ptr("project-1")},
				}}},
			},
			projectID:   "project-1",
			projectName: "prod",
			secretCache: make(map[string]string),
		}

		_, err := loader.GetSecret("MEXC_API_KEY")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "secret 'MEXC_API_KEY' not found")
	})
}

func ptr(v string) *string {
	return &v
}

type fakeBitwardenClient struct {
	projects sdk.ProjectsInterface
	secrets  sdk.SecretsInterface
}

func (c *fakeBitwardenClient) AccessTokenLogin(string, *string) error { return nil }
func (c *fakeBitwardenClient) Projects() sdk.ProjectsInterface        { return c.projects }
func (c *fakeBitwardenClient) Secrets() sdk.SecretsInterface          { return c.secrets }
func (c *fakeBitwardenClient) Generators() sdk.GeneratorsInterface    { return nil }
func (c *fakeBitwardenClient) Close()                                 {}

type fakeProjects struct {
	list *sdk.ProjectsResponse
	err  error
}

func (p *fakeProjects) Create(string, string) (*sdk.ProjectResponse, error) {
	return nil, nil
}
func (p *fakeProjects) List(string) (*sdk.ProjectsResponse, error) {
	return p.list, p.err
}
func (p *fakeProjects) Get(string) (*sdk.ProjectResponse, error) {
	return nil, nil
}
func (p *fakeProjects) Update(string, string, string) (*sdk.ProjectResponse, error) {
	return nil, nil
}
func (p *fakeProjects) Delete([]string) (*sdk.ProjectsDeleteResponse, error) {
	return nil, nil
}

type fakeSecrets struct {
	sync *sdk.SecretsSyncResponse
	err  error
}

func (s *fakeSecrets) Create(string, string, string, string, []string) (*sdk.SecretResponse, error) {
	return nil, nil
}
func (s *fakeSecrets) List(string) (*sdk.SecretIdentifiersResponse, error) {
	return nil, nil
}
func (s *fakeSecrets) Get(string) (*sdk.SecretResponse, error) {
	return nil, nil
}
func (s *fakeSecrets) GetByIDS([]string) (*sdk.SecretsResponse, error) {
	return nil, nil
}
func (s *fakeSecrets) Update(string, string, string, string, string, []string) (*sdk.SecretResponse, error) {
	return nil, nil
}
func (s *fakeSecrets) Delete([]string) (*sdk.SecretsDeleteResponse, error) {
	return nil, nil
}
func (s *fakeSecrets) Sync(string, *time.Time) (*sdk.SecretsSyncResponse, error) {
	return s.sync, s.err
}
