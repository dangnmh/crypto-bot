package version_test

import (
	"testing"

	"crypto-bot/pkg/version"

	"github.com/stretchr/testify/assert"
)

func TestVersionDefaults(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "dev", version.Version)
	assert.Equal(t, "none", version.Commit)
	assert.Equal(t, "unknown", version.BuildTime)
}
