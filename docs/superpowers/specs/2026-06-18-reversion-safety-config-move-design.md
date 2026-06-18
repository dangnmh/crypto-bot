# Design Spec: Reversion Safety Configuration Move

Move safety-related configuration parameters (`maxLatency`, `minFundingRate`, `maxPriceDiffPercent`) under the `"safety"` block in the reversion configuration files.

## 1. Goal

Group safety threshold/limit variables together into a single, cohesive `"safety"` block in the JSONC configuration files. This simplifies configuration maintenance and cleans up the root config namespaces.

## 2. Proposed Changes

### Configuration Schema

#### [configs/funding/prod/reversion.jsonc](file:///home/four/projects/crypto-bot/configs/funding/prod/reversion.jsonc) and [configs/funding/local/reversion.jsonc](file:///home/four/projects/crypto-bot/configs/funding/local/reversion.jsonc)
- Move `maxLatency`, `minFundingRate`, and `maxPriceDiffPercent` from the root namespace to the nested `"safety"` block:
```jsonc
  "safety": {
    "minVol24USD": 20000000,
    "maxImpactRatio": 5,
    "maxLatency": "400ms",
    "minFundingRate": 0.8,
    "maxPriceDiffPercent": 0.8
  }
```

### Go Config Mapping

#### [internal/bots/funding/config/types.go](file:///home/four/projects/crypto-bot/internal/bots/funding/config/types.go)
- Remove `MaxLatency`, `MinFundingRate`, and `MaxPriceDiffPercent` from `RawFundingReversionConfig`.

#### [internal/bots/funding/config/system.go](file:///home/four/projects/crypto-bot/internal/bots/funding/config/system.go)
- Add them to `SafetyConfig`:
```go
type SafetyConfig struct {
	MinVol24USD         float64        `json:"minVol24USD"`
	MaxImpactRatio      float64        `json:"maxImpactRatio"`
	MaxLatency          types.Duration `json:"maxLatency"`
	MinFundingRate      float64        `json:"minFundingRate"`
	MaxPriceDiffPercent float64        `json:"maxPriceDiffPercent"`
}
```

#### [internal/bots/funding/config/config.go](file:///home/four/projects/crypto-bot/internal/bots/funding/config/config.go)
- Update `applyDefaults` method to read `MaxLatency`, `MinFundingRate`, and `MaxPriceDiffPercent` from `c.Reversion.Safety` instead of the passed flat `RawFundingReversionConfig` argument:
```go
	defaultFloat(&sc.MaxPriceDiffPercent, c.Reversion.Safety.MaxPriceDiffPercent)
	defaultFloat(&sc.MinFundingRate, c.Reversion.Safety.MinFundingRate)
	// ...
	if !sc.FundingReversion.Enabled && d.Enabled {
		sc.FundingReversion.Enabled = true
		sc.FundingReversion.MaxLatency = c.Reversion.Safety.MaxLatency
		// ...
	} else if sc.FundingReversion.Enabled {
		defaultDuration(&sc.FundingReversion.MaxLatency, c.Reversion.Safety.MaxLatency)
		// ...
	}
```

### Unit Tests updates

#### [internal/bots/funding/config/config_test.go](file:///home/four/projects/crypto-bot/internal/bots/funding/config/config_test.go)
- Replace `config.RawFundingReversionConfig` as default configurations parameter with a unified `testDefaults` structure containing both `RawFundingReversionConfig` and `SafetyConfig`.
- Map those properties into `mockRev.Safety` inside `loadWith` helper.

#### [internal/bots/funding/application/funding_bot_internal_test.go](file:///home/four/projects/crypto-bot/internal/bots/funding/application/funding_bot_internal_test.go)
- Set `Scanners: config.ScannersConfig{Configured: true}` in the test configuration of `TestNewFundingBotBuildsExchangeScopedResources` to ensure MEXC store is properly built, fixing pre-existing test failure.

## 3. Verification Plan

### Automated Tests
Run unit tests to verify configuration loading and validation works correctly:
```bash
make test
```
