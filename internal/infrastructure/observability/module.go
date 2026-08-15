package observability

import (
	"go.uber.org/fx"
)

// Module wires observability and metrics dependencies.
var Module = fx.Options(
	fx.Provide(
		InitMetrics,
	),
)
