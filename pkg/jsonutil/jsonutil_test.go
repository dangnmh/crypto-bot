package jsonutil_test

import (
	"testing"

	"crypto-bot/pkg/jsonutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnmarshal(t *testing.T) {
	t.Parallel()

	type payload struct {
		Name string `json:"name"`
	}
	got, err := jsonutil.Unmarshal[payload]([]byte(`{"name":"btc"}`))
	require.NoError(t, err)
	assert.Equal(t, "btc", got.Name)

	_, err = jsonutil.Unmarshal[payload]([]byte(`{`))
	require.Error(t, err)
}
