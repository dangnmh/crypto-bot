package bitunix

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWsAdapter_GenerateLoginSignature(t *testing.T) {
	t.Parallel()

	adapter := &WsAdapter{
		apiKey:    "my-key",
		apiSecret: "my-secret",
	}

	sig := adapter.generateLoginSignature("nonce123", 1747402389682)
	assert.NotEmpty(t, sig)
}
