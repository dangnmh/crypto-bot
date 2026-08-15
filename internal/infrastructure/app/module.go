package app

import (
	"go.uber.org/fx"
)

// Module wires application runner lifecycle.
var Module = fx.Options(
	fx.Provide(
		NewBotRunner,
	),
	fx.Invoke(
		RegisterBotRunner,
	),
)
