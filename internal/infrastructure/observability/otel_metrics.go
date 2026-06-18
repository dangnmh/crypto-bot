package observability

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/fx"
)

// InitMetrics initializes OpenTelemetry metrics with a Prometheus exporter reader.
func InitMetrics(lc fx.Lifecycle) (http.Handler, error) {
	exporter, err := otelprom.New()
	if err != nil {
		return nil, fmt.Errorf("failed to create prometheus exporter: %w", err)
	}

	provider := metric.NewMeterProvider(
		metric.WithReader(exporter),
	)

	otel.SetMeterProvider(provider)

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := runtime.Start(); err != nil {
				return fmt.Errorf("failed to start runtime metrics: %w", err)
			}
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return provider.Shutdown(ctx)
		},
	})

	return promhttp.Handler(), nil
}
