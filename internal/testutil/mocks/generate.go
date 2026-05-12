package mocks

//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -destination mock_exchange.go -package mocks -typed crypto-bot/internal/infrastructure/exchange Client
//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -destination mock_store.go -package mocks -typed crypto-bot/internal/infrastructure/store TickerReader,ContractReader,PriceReader,FundingReader,DepthReader,KlineReadWriter
//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -destination mock_ws.go -package mocks -typed crypto-bot/internal/infrastructure/ws Subscriber
//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -destination mock_watcher.go -package mocks -typed crypto-bot/internal/infrastructure/watcher OrderNotifier
//go:generate go run go.uber.org/mock/mockgen@v0.6.0 -destination mock_clock.go -package mocks -typed crypto-bot/internal/domain Clock
