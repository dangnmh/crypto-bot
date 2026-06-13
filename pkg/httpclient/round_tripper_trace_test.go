package httpclient_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"crypto-bot/pkg/httpclient"
	"crypto-bot/pkg/tracectx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTraceRoundTripper_InjectsHeaders(t *testing.T) {
	t.Parallel()

	expectedID := "test-trace-id-123"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, expectedID, r.Header.Get("X-Request-ID"))
		assert.Equal(t, expectedID, r.Header.Get("req_id"))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// 1. Test with Correlation ID in context
	ctx := tracectx.WithCorrelationIDValue(context.Background(), expectedID)

	client := httpclient.NewPool(httpclient.DefaultPoolConfig())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	// 2. Test with Reversion ID in context
	ctx2 := tracectx.WithReversionID(context.Background(), expectedID)
	req2, err := http.NewRequestWithContext(ctx2, http.MethodGet, server.URL, http.NoBody)
	require.NoError(t, err)

	resp2, err := client.Do(req2)
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp2.StatusCode)
}
