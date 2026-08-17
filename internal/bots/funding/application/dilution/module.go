package dilution

import (
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/trading/ordermanager"

	"go.uber.org/fx"
)

// Module registers the dilution components in the Fx dependency container.
var Module = fx.Options(
	fx.Provide(
		provideEngineGetter,
		provideOrderDispatcher,
		NewDilutionMaker,
		NewDilutionRunner,
		NewDilutionJob,
	),
)

func provideEngineGetter(engine *infraapp.Engine) EngineProviderGetter {
	return engine
}

func provideOrderDispatcher(om *ordermanager.OrderManager) OrderManagerDispatcher {
	return om
}
