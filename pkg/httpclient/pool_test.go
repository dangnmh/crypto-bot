package httpclient_test

import (
	"net/http"
	"testing"

	httpclient "crypto-bot/pkg/httpclient"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPoolConfig(t *testing.T) {
	t.Parallel()
	cfg := httpclient.DefaultPoolConfig()

	assert.Equal(t, 100, cfg.MaxIdleConns)
	assert.Equal(t, 100, cfg.MaxIdleConnsPerHost)
	assert.Equal(t, 90*time.Second, cfg.IdleConnTimeout)
	assert.Equal(t, 5*time.Second, cfg.TLSHandshakeTimeout)
	assert.False(t, cfg.DisableCompression)
	assert.Equal(t, 10*time.Second, cfg.Timeout)
}

func TestNewPool(t *testing.T) {
	t.Parallel()
	cfg := httpclient.DefaultPoolConfig()
	client := httpclient.NewPool(cfg)

	require.NotNil(t, client, "expected non-nil client")
	assert.Equal(t, cfg.Timeout, client.Timeout)

	transport, ok := client.Transport.(*http.Transport)
	require.True(t, ok, "expected *http.Transport")

	assert.Equal(t, cfg.MaxIdleConns, transport.MaxIdleConns)
	assert.Equal(t, cfg.MaxIdleConnsPerHost, transport.MaxIdleConnsPerHost)
	assert.Equal(t, cfg.IdleConnTimeout, transport.IdleConnTimeout)
	assert.Equal(t, cfg.TLSHandshakeTimeout, transport.TLSHandshakeTimeout)
	assert.Equal(t, cfg.DisableCompression, transport.DisableCompression)
}

func TestNewPool_CustomConfig(t *testing.T) {
	t.Parallel()
	cfg := httpclient.PoolConfig{
		MaxIdleConns:        50,
		MaxIdleConnsPerHost: 25,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 3 * time.Second,
		DisableCompression:  true,
		Timeout:             5 * time.Second,
	}

	client := httpclient.NewPool(cfg)
	assert.Equal(t, 5*time.Second, client.Timeout)
	transport, _ := client.Transport.(*http.Transport)
	assert.Equal(t, 50, transport.MaxIdleConns)
	assert.True(t, transport.DisableCompression)
}
