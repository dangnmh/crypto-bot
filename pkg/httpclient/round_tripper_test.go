package httpclient_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"crypto-bot/pkg/httpclient"

	"github.com/hashicorp/go-retryablehttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetry_Get429_RetryAfter_Seconds(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := attempts.Add(1)
		assert.Equal(t, http.MethodGet, r.Method)

		if current < 3 {
			w.Header().Set("Retry-After", "1") // 1 second
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte("rate limited"))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("success"))
	}))
	defer server.Close()

	cfg := httpclient.DefaultPoolConfig()
	cfg.RetryMaxAttempts = 3
	client := httpclient.NewPool(cfg)

	start := time.Now()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, http.NoBody)
	require.NoError(t, err)
	resp, err := client.Do(req)
	duration := time.Since(start)

	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(3), attempts.Load())
	// We expect 2 retries, each waiting 1s, so total duration should be >= 2s
	assert.GreaterOrEqual(t, duration, 2*time.Second)
}

func TestRetry_Get429_Fallback1s(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current := attempts.Add(1)
		if current < 2 {
			// No Retry-After header
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := httpclient.DefaultPoolConfig()
	cfg.RetryMaxAttempts = 2
	client := httpclient.NewPool(cfg)

	if rt, ok := client.Transport.(*retryablehttp.RoundTripper); ok {
		rt.Client.RetryWaitMin = 100 * time.Millisecond
		rt.Client.RetryWaitMax = 100 * time.Millisecond
	}

	start := time.Now()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, http.NoBody)
	require.NoError(t, err)
	resp, err := client.Do(req)
	duration := time.Since(start)

	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), attempts.Load())
	// 1 retry waiting 100ms fallback, so total duration >= 100ms
	assert.GreaterOrEqual(t, duration, 100*time.Millisecond)
}

func TestRetry_Post429_NoRetry(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		assert.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	cfg := httpclient.DefaultPoolConfig()
	cfg.RetryMaxAttempts = 3
	client := httpclient.NewPool(cfg)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, server.URL, bytes.NewBufferString("body"))
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, int32(1), attempts.Load()) // POST should not retry
}

func TestRetry_Get500_NoRetry(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("X-Custom-Error", "500-error-check")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("mocked 500 error code body"))
	}))
	defer server.Close()

	cfg := httpclient.DefaultPoolConfig()
	cfg.RetryMaxAttempts = 3
	client := httpclient.NewPool(cfg)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	assert.Equal(t, int32(1), attempts.Load()) // HTTP 500 on GET should not retry
}

func TestRetry_MaxAttempts(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()

	cfg := httpclient.DefaultPoolConfig()
	cfg.RetryMaxAttempts = 2 // max 2 retries = total 3 attempts
	client := httpclient.NewPool(cfg)

	if rt, ok := client.Transport.(*retryablehttp.RoundTripper); ok {
		rt.Client.RetryWaitMin = 10 * time.Millisecond
		rt.Client.RetryWaitMax = 10 * time.Millisecond
	}

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL, http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, int32(3), attempts.Load()) // 1 initial + 2 retries = 3 total attempts
}
