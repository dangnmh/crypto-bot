package network_test

import (
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/network"
	"crypto-bot/pkg/httpclient"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveLocalTCPAddr(t *testing.T) {
	t.Parallel()

	t.Run("empty string returns nil without error", func(t *testing.T) {
		t.Parallel()
		addr, err := network.ResolveLocalTCPAddr("")
		require.NoError(t, err)
		assert.Nil(t, addr)
	})

	t.Run("whitespace returns nil without error", func(t *testing.T) {
		t.Parallel()
		addr, err := network.ResolveLocalTCPAddr("   ")
		require.NoError(t, err)
		assert.Nil(t, addr)
	})

	t.Run("valid IPv4 resolves to TCPAddr with port 0", func(t *testing.T) {
		t.Parallel()
		addr, err := network.ResolveLocalTCPAddr("127.0.0.1")
		require.NoError(t, err)
		require.NotNil(t, addr)
		assert.Equal(t, "127.0.0.1:0", addr.String())
	})

	t.Run("invalid IP returns error", func(t *testing.T) {
		t.Parallel()
		addr, err := network.ResolveLocalTCPAddr("999.999.999.999")
		assert.Error(t, err)
		assert.Nil(t, addr)
	})
}

func TestParseProxyURL(t *testing.T) {
	t.Parallel()

	t.Run("empty string returns nil", func(t *testing.T) {
		t.Parallel()
		u, err := network.ParseProxyURL("")
		require.NoError(t, err)
		assert.Nil(t, u)
	})

	t.Run("valid http proxy", func(t *testing.T) {
		t.Parallel()
		u, err := network.ParseProxyURL("http://proxy.example.com:8080")
		require.NoError(t, err)
		require.NotNil(t, u)
		assert.Equal(t, "http", u.Scheme)
		assert.Equal(t, "proxy.example.com:8080", u.Host)
	})

	t.Run("valid socks5 proxy", func(t *testing.T) {
		t.Parallel()
		u, err := network.ParseProxyURL("socks5://user:pass@127.0.0.1:1080")
		require.NoError(t, err)
		require.NotNil(t, u)
		assert.Equal(t, "socks5", u.Scheme)
		assert.Equal(t, "127.0.0.1:1080", u.Host)
	})
}

func TestNewAccountHTTPClient(t *testing.T) {
	t.Parallel()

	t.Run("creates client with default settings when outbound IP empty", func(t *testing.T) {
		t.Parallel()
		cfg := httpclient.DefaultPoolConfig()
		client, err := network.NewAccountHTTPClient("", "", cfg)
		require.NoError(t, err)
		require.NotNil(t, client)
	})

	t.Run("creates client with LocalAddr when outbound IP provided", func(t *testing.T) {
		t.Parallel()
		cfg := httpclient.DefaultPoolConfig()
		client, err := network.NewAccountHTTPClient("127.0.0.1", "", cfg)
		require.NoError(t, err)
		require.NotNil(t, client)
	})

	t.Run("fails on invalid outbound IP", func(t *testing.T) {
		t.Parallel()
		cfg := httpclient.DefaultPoolConfig()
		client, err := network.NewAccountHTTPClient("invalid-ip", "", cfg)
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

func TestNewAccountWSDialer(t *testing.T) {
	t.Parallel()

	t.Run("creates dialer without LocalAddr when outbound IP empty", func(t *testing.T) {
		t.Parallel()
		dialer, err := network.NewAccountWSDialer("", "", 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, dialer)
		assert.Nil(t, dialer.NetDialContext)
	})

	t.Run("creates dialer with NetDialContext when outbound IP provided", func(t *testing.T) {
		t.Parallel()
		dialer, err := network.NewAccountWSDialer("127.0.0.1", "", 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, dialer)
		assert.NotNil(t, dialer.NetDialContext)
	})

	t.Run("creates dialer with Proxy", func(t *testing.T) {
		t.Parallel()
		dialer, err := network.NewAccountWSDialer("", "http://proxy.example.com:8080", 5*time.Second)
		require.NoError(t, err)
		require.NotNil(t, dialer)
		assert.NotNil(t, dialer.Proxy)
	})
}
