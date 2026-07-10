package exchange

import (
	"log/slog"
	"net/http"

	"crypto-bot/pkg/httpclient"

	"go.uber.org/fx"
)

// Module is the Fx option that provides exchange infrastructure dependencies.
var Module = fx.Options(
	fx.Provide(
		ProvideHTTPClient,
	),
)

// ProvideHTTPClient instantiates a shared HTTP client pool for exchange integrations.
func ProvideHTTPClient(log *slog.Logger) *http.Client {
	cfg := httpclient.DefaultPoolConfig()
	cfg.Logger = log
	return httpclient.NewPool(cfg)
}
