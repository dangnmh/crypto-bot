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

func assertHeaderCasePreserved(t *testing.T, header http.Header, key, expected string) {
	t.Helper()
	canonicalKey := http.CanonicalHeaderKey(key)
	found := false
	for k, vals := range header {
		if k == canonicalKey {
			found = true
			if assert.Len(t, vals, 1) {
				assert.Equal(t, expected, vals[0])
			}
			break
		}
	}
	assert.True(t, found, "header key %q (canonical: %q) not found", key, canonicalKey)
}

func newTraceTestServer(t *testing.T, expectedID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, expectedID, r.Header.Get("X-Request-ID"))
		assert.Equal(t, expectedID, r.Header.Get("req_id"))
		assert.Equal(t, expectedID, r.Header.Get("request_id"))
		assert.Equal(t, expectedID, r.Header.Get("requestid"))

		assertHeaderCasePreserved(t, r.Header, "X-Request-ID", expectedID)
		assertHeaderCasePreserved(t, r.Header, "req_id", expectedID)
		assertHeaderCasePreserved(t, r.Header, "request_id", expectedID)
		assertHeaderCasePreserved(t, r.Header, "requestid", expectedID)

		w.WriteHeader(http.StatusOK)
	}))
}

func TestTraceRoundTripper_InjectsHeaders(t *testing.T) {
	t.Parallel()

	expectedID := "test-trace-id-123"
	server := newTraceTestServer(t, expectedID)
	defer server.Close()

	// Test with Request ID in context
	ctx := tracectx.WithRequestIDValue(context.Background(), expectedID)

	client := httpclient.NewPool(httpclient.DefaultPoolConfig())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestWrapWithRequestID_InjectsHeaders(t *testing.T) {
	t.Parallel()

	expectedID := "test-wrap-id-456"
	server := newTraceTestServer(t, expectedID)
	defer server.Close()

	ctx := tracectx.WithRequestIDValue(context.Background(), expectedID)

	transport := httpclient.WrapWithRequestID(http.DefaultTransport)
	client := &http.Client{Transport: transport}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL, http.NoBody)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
