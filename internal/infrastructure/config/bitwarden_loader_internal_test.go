package config

import (
	"errors"
	"testing"
	"time"

	"github.com/bitwarden/sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBitwardenClient struct {
	projects sdk.ProjectsInterface
	secrets  sdk.SecretsInterface
}

func (f fakeBitwardenClient) AccessTokenLogin(_ string, _ *string) error { return nil }
func (f fakeBitwardenClient) Projects() sdk.ProjectsInterface            { return f.projects }
func (f fakeBitwardenClient) Secrets() sdk.SecretsInterface              { return f.secrets }
func (f fakeBitwardenClient) Generators() sdk.GeneratorsInterface        { return nil }
func (f fakeBitwardenClient) Close()                                     {}

type fakeProjects struct {
	resp *sdk.ProjectsResponse
	err  error
}

func (f fakeProjects) Create(string, string) (*sdk.ProjectResponse, error) { return nil, nil }
func (f fakeProjects) List(string) (*sdk.ProjectsResponse, error)          { return f.resp, f.err }
func (f fakeProjects) Get(string) (*sdk.ProjectResponse, error)            { return nil, nil }
func (f fakeProjects) Update(string, string, string) (*sdk.ProjectResponse, error) {
	return nil, nil
}
func (f fakeProjects) Delete([]string) (*sdk.ProjectsDeleteResponse, error) { return nil, nil }

type fakeSecrets struct {
	resp      *sdk.SecretsSyncResponse
	err       error
	syncCalls int
}

func (f *fakeSecrets) Create(string, string, string, string, []string) (*sdk.SecretResponse, error) {
	return nil, nil
}
func (f *fakeSecrets) List(string) (*sdk.SecretIdentifiersResponse, error) { return nil, nil }
func (f *fakeSecrets) Get(string) (*sdk.SecretResponse, error)             { return nil, nil }
func (f *fakeSecrets) GetByIDS([]string) (*sdk.SecretsResponse, error)     { return nil, nil }
func (f *fakeSecrets) Update(string, string, string, string, string, []string) (*sdk.SecretResponse, error) {
	return nil, nil
}
func (f *fakeSecrets) Delete([]string) (*sdk.SecretsDeleteResponse, error) { return nil, nil }
func (f *fakeSecrets) Sync(string, *time.Time) (*sdk.SecretsSyncResponse, error) {
	f.syncCalls++
	return f.resp, f.err
}

func TestNewBitwardenLoaderValidatesEnvironment(t *testing.T) {
	t.Setenv("BITWARDEN_ACCESS_TOKEN", "")
	t.Setenv("BITWARDEN_ORGANIZATION_ID", "org")
	t.Setenv("BITWARDEN_PROJECT_NAME", "project")
	_, err := NewBitwardenLoader()
	require.ErrorContains(t, err, "BITWARDEN_ACCESS_TOKEN")

	t.Setenv("BITWARDEN_ACCESS_TOKEN", "token")
	t.Setenv("BITWARDEN_ORGANIZATION_ID", "")
	_, err = NewBitwardenLoader()
	require.ErrorContains(t, err, "BITWARDEN_ORGANIZATION_ID")

	t.Setenv("BITWARDEN_ORGANIZATION_ID", "org")
	t.Setenv("BITWARDEN_PROJECT_NAME", "")
	_, err = NewBitwardenLoader()
	require.ErrorContains(t, err, "BITWARDEN_PROJECT_NAME")
}

func TestBitwardenLoaderResolveProjectID(t *testing.T) {
	t.Parallel()

	loader := &BitwardenLoader{
		client: fakeBitwardenClient{projects: fakeProjects{resp: &sdk.ProjectsResponse{
			Data: []sdk.ProjectResponse{{ID: "project-id", Name: "funding"}},
		}}},
		organizationID: "org",
		projectName:    "funding",
		secretCache:    map[string]string{},
	}

	id, err := loader.resolveProjectID()
	require.NoError(t, err)
	assert.Equal(t, "project-id", id)

	loader.client = fakeBitwardenClient{projects: fakeProjects{err: errors.New("list failed")}}
	id, err = loader.resolveProjectID()
	require.NoError(t, err)
	assert.Equal(t, "project-id", id)
}

func TestBitwardenLoaderResolveProjectIDErrors(t *testing.T) {
	t.Parallel()

	loader := &BitwardenLoader{
		client:         fakeBitwardenClient{projects: fakeProjects{err: errors.New("list failed")}},
		organizationID: "org",
		projectName:    "funding",
		secretCache:    map[string]string{},
	}
	_, err := loader.resolveProjectID()
	require.ErrorContains(t, err, "failed to list projects")

	loader.client = fakeBitwardenClient{projects: fakeProjects{resp: &sdk.ProjectsResponse{
		Data: []sdk.ProjectResponse{{ID: "other-id", Name: "other"}},
	}}}
	_, err = loader.resolveProjectID()
	require.ErrorContains(t, err, "project 'funding' not found")
}

func TestBitwardenLoaderGetSecretCachesProjectSecret(t *testing.T) {
	t.Parallel()

	projectID := "project-id"
	secrets := &fakeSecrets{resp: &sdk.SecretsSyncResponse{
		Secrets: []sdk.SecretResponse{
			{Key: "OTHER", Value: "skip", ProjectID: &projectID},
			{Key: "MEXC_API_KEY", Value: "key", ProjectID: &projectID},
			{Key: "MEXC_API_KEY", Value: "wrong-project"},
		},
	}}
	loader := &BitwardenLoader{
		client: fakeBitwardenClient{
			projects: fakeProjects{resp: &sdk.ProjectsResponse{Data: []sdk.ProjectResponse{{ID: projectID, Name: "funding"}}}},
			secrets:  secrets,
		},
		organizationID: "org",
		projectName:    "funding",
		secretCache:    map[string]string{},
	}

	value, err := loader.GetSecret("MEXC_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "key", value)

	value, err = loader.GetSecret("MEXC_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "key", value)
	assert.Equal(t, 1, secrets.syncCalls)
}

func TestBitwardenLoaderGetSecretErrors(t *testing.T) {
	t.Parallel()

	projectID := "project-id"
	loader := &BitwardenLoader{
		client: fakeBitwardenClient{
			projects: fakeProjects{resp: &sdk.ProjectsResponse{Data: []sdk.ProjectResponse{{ID: projectID, Name: "funding"}}}},
			secrets:  &fakeSecrets{err: errors.New("sync failed")},
		},
		organizationID: "org",
		projectName:    "funding",
		secretCache:    map[string]string{},
	}
	_, err := loader.GetSecret("MEXC_API_KEY")
	require.ErrorContains(t, err, "failed to sync secrets")

	loader.client = fakeBitwardenClient{
		projects: fakeProjects{resp: &sdk.ProjectsResponse{Data: []sdk.ProjectResponse{{ID: projectID, Name: "funding"}}}},
		secrets:  &fakeSecrets{resp: &sdk.SecretsSyncResponse{Secrets: []sdk.SecretResponse{{Key: "OTHER", Value: "x", ProjectID: &projectID}}}},
	}
	_, err = loader.GetSecret("MEXC_API_KEY")
	require.ErrorContains(t, err, "secret 'MEXC_API_KEY' not found")
}
