package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/bitwarden/sdk-go/v2"
)

// BitwardenLoader loads secrets from Bitwarden Secrets Manager.
type BitwardenLoader struct {
	client         sdk.BitwardenClientInterface
	accessToken    string
	organizationID string
	projectName    string
	projectID      string
	secretCache    map[string]string
}

// NewBitwardenLoader creates a new Bitwarden secrets loader.
// Environment variables required:
//   - BITWARDEN_ACCESS_TOKEN: Service account access token
//   - BITWARDEN_ORGANIZATION_ID: Organization ID
//   - BITWARDEN_PROJECT_NAME: Project name (will resolve to project ID)
func NewBitwardenLoader() (*BitwardenLoader, error) {
	accessToken := os.Getenv("BITWARDEN_ACCESS_TOKEN")
	if accessToken == "" {
		return nil, fmt.Errorf("BITWARDEN_ACCESS_TOKEN is required")
	}

	organizationID := os.Getenv("BITWARDEN_ORGANIZATION_ID")
	if organizationID == "" {
		return nil, fmt.Errorf("BITWARDEN_ORGANIZATION_ID is required")
	}

	projectName := os.Getenv("BITWARDEN_PROJECT_NAME")
	if projectName == "" {
		return nil, fmt.Errorf("BITWARDEN_PROJECT_NAME is required")
	}

	// Trim whitespace
	accessToken = strings.TrimSpace(accessToken)
	organizationID = strings.TrimSpace(organizationID)
	projectName = strings.TrimSpace(projectName)

	client, err := sdk.NewBitwardenClient(nil, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Bitwarden client: %w", err)
	}

	// stateFile configuration available if needed: os.Getenv("HOME") + "/.bitwarden/state.json"
	if err := client.AccessTokenLogin(accessToken, nil); err != nil {
		return nil, fmt.Errorf("failed to login to Bitwarden: %w", err)
	}

	return &BitwardenLoader{
		client:         client,
		accessToken:    accessToken,
		organizationID: organizationID,
		projectName:    projectName,
		secretCache:    make(map[string]string),
	}, nil
}

// resolveProjectID gets the project ID from project name.
func (b *BitwardenLoader) resolveProjectID() (string, error) {
	if b.projectID != "" {
		return b.projectID, nil
	}

	projectsResp, err := b.client.Projects().List(b.organizationID)
	if err != nil {
		return "", fmt.Errorf("failed to list projects: %w", err)
	}

	for _, p := range projectsResp.Data {
		if p.Name == b.projectName {
			b.projectID = p.ID
			return p.ID, nil
		}
	}

	return "", fmt.Errorf("project '%s' not found in organization %s", b.projectName, b.organizationID)
}

// GetSecret retrieves a secret by key, caching the result.
// This uses the Sync endpoint to get full secret details including project associations.
func (b *BitwardenLoader) GetSecret(secretKey string) (string, error) {
	if cached, ok := b.secretCache[secretKey]; ok {
		return cached, nil
	}

	projectID, err := b.resolveProjectID()
	if err != nil {
		return "", err
	}

	// Use Sync to get full secret details with project associations
	secretsSyncResp, err := b.client.Secrets().Sync(b.organizationID, nil)
	if err != nil {
		return "", fmt.Errorf("failed to sync secrets: %w", err)
	}

	for i := range secretsSyncResp.Secrets {
		s := &secretsSyncResp.Secrets[i]
		// Match by key
		if s.Key == secretKey {
			// Check if secret belongs to our project (ProjectID is a pointer)
			if s.ProjectID != nil && *s.ProjectID == projectID {
				b.secretCache[secretKey] = s.Value
				return s.Value, nil
			}
		}
	}

	return "", fmt.Errorf("secret '%s' not found in project '%s'", secretKey, b.projectName)
}
