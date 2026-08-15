package server

import (
	"go.uber.org/fx"
)

// Module wires APIServer dependencies and lifecycle registration.
var Module = fx.Options(
	fx.Provide(
		NewAPIServer,
	),
	fx.Invoke(
		Register,
	),
)
