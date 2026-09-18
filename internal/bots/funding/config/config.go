package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	sysconfig "crypto-bot/internal/infrastructure/config"
	pkgconfig "crypto-bot/pkg/config"
	"crypto-bot/pkg/types"

	"github.com/go-playground/validator/v10"
)

// LoadAndValidate reads a config file and validates it, supporting both structs and slices.
func LoadAndValidate[T any](path string) (*T, error) {
	cfg, err := pkgconfig.Load[T](path)
	if err != nil {
		return nil, err
	}

	validate := newValidator()

	val := reflect.ValueOf(cfg)
	if val.Kind() == reflect.Pointer {
		val = val.Elem()
	}

	typeName := reflect.TypeFor[T]().Name()
	if typeName != "SystemConfig" && typeName != "FundingConfig" && typeName != "ReversionConfig" && typeName != "AccountReversionConfig" {
		if val.Kind() == reflect.Struct {
			if err := validate.Struct(cfg); err != nil {
				return nil, fmt.Errorf("validation failed: %w", err)
			}
		} else if val.Kind() == reflect.Slice {
			for i := 0; i < val.Len(); i++ {
				item := val.Index(i).Interface()
				if err := validate.Struct(item); err != nil {
					return nil, fmt.Errorf("validation failed at index %d: %w", i, err)
				}
			}
		}
	}

	return cfg, nil
}

// Load reads configuration files using specific paths and returns the Config.
// Load loads multi-account configurations from an accounts manifest file and validates all configurations.
// All configurations and credentials must be explicitly provided without fallback defaults.
func Load(sysCfg *SystemConfig, accountsPath, blacklistPath, commonReversionPath string) (*Config, error) {
	paths := LoadPaths{
		AccountsPath:        accountsPath,
		BlacklistPath:       blacklistPath,
		CommonReversionPath: commonReversionPath,
	}
	validate := newValidator()
	if err := validate.Struct(paths); err != nil {
		return nil, fmt.Errorf("config paths validation: %w", err)
	}

	manifest, err := sysconfig.LoadAccountsManifest(accountsPath)
	if err != nil {
		return nil, fmt.Errorf("load accounts manifest: %w", err)
	}

	commonReversion, err := resolveCommonReversion(commonReversionPath)
	if err != nil {
		return nil, fmt.Errorf("parse reversion config: %w", err)
	}

	blk, err := LoadAndValidate[BlacklistConfig](blacklistPath)
	if err != nil {
		return nil, fmt.Errorf("parse blacklist config: %w", err)
	}

	cfg := &Config{
		System:          sysCfg,
		CommonReversion: commonReversion,
		Accounts:        make(map[string]*AccountBotConfig),
		Blacklist:       blk,
	}

	accountsDir := filepath.Dir(accountsPath)
	for i := range manifest.Accounts {
		acc := &manifest.Accounts[i]
		if !acc.Enabled {
			continue
		}
		if strings.TrimSpace(acc.ID) == "" {
			return nil, fmt.Errorf("manifest accounts[%d]: account id is required", i)
		}
		accBotCfg, err := LoadAccountBotConfig(*acc, commonReversion, accountsDir)
		if err != nil {
			return nil, err
		}
		cfg.Accounts[acc.ID] = accBotCfg
	}

	if len(cfg.Accounts) == 0 {
		return nil, fmt.Errorf("at least one enabled account must be present in %s", accountsPath)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config validation: %w", err)
	}

	return cfg, nil
}

func resolveCommonReversion(commonReversionPath string) (*ReversionConfig, error) {
	cr, err := LoadAndValidate[ReversionConfig](commonReversionPath)
	if err != nil {
		return nil, fmt.Errorf("load common reversion config from %s: %w", commonReversionPath, err)
	}
	applyReversionDefaults(cr)
	return cr, nil
}

// LoadMultiAccount is an alias to Load for multi-account manifest loading.
func LoadMultiAccount(sysCfg *SystemConfig, accountsPath, blacklistPath, commonReversionPath string) (*Config, error) {
	return Load(sysCfg, accountsPath, blacklistPath, commonReversionPath)
}

func resolveAccountConfigPath(baseDir, rawPath string) string {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" || filepath.IsAbs(rawPath) {
		return rawPath
	}
	if baseDir != "" {
		rel := filepath.Join(baseDir, rawPath)
		if _, err := os.Stat(rel); err == nil {
			return rel
		}

		// Also support flattened filenames for Kubernetes ConfigMaps where slashes are replaced by dots or underscores
		cleanRel := strings.TrimPrefix(filepath.Clean(rawPath), "."+string(filepath.Separator))
		cleanRel = strings.TrimPrefix(cleanRel, string(filepath.Separator))
		flatDot := filepath.Join(baseDir, strings.ReplaceAll(cleanRel, "/", "."))
		if _, err := os.Stat(flatDot); err == nil {
			return flatDot
		}
		flatUnder := filepath.Join(baseDir, strings.ReplaceAll(cleanRel, "/", "_"))
		if _, err := os.Stat(flatUnder); err == nil {
			return flatUnder
		}
	}
	if _, err := os.Stat(rawPath); err == nil {
		return rawPath
	}
	if baseDir != "" {
		return filepath.Join(baseDir, rawPath)
	}
	return rawPath
}

func getRequiredAccountConfigPath(acc sysconfig.AccountConfig, key string, baseDir ...string) (string, error) {
	p, ok := acc.Configs[key]
	if !ok || strings.TrimSpace(p) == "" {
		return "", fmt.Errorf("account %q missing %s config in configs.%s", acc.ID, key, key)
	}
	if len(baseDir) > 0 && baseDir[0] != "" {
		return resolveAccountConfigPath(baseDir[0], p), nil
	}
	return p, nil
}

// LoadAccountBotConfig loads and validates isolated configs for a single account.
// An optional commonReversion can be provided as a base to inherit common settings.
// An optional baseDir can be provided to resolve account config paths if they are relative.
func LoadAccountBotConfig(acc sysconfig.AccountConfig, commonReversion *ReversionConfig, baseDir ...string) (*AccountBotConfig, error) {
	if strings.TrimSpace(acc.ID) == "" {
		return nil, fmt.Errorf("account id is required")
	}
	accCfg := &AccountBotConfig{
		Account: acc,
	}

	reversionPath, err := getRequiredAccountConfigPath(acc, "reversion", baseDir...)
	if err != nil {
		return nil, err
	}
	accRevCfg, err := LoadAndValidate[AccountReversionConfig](reversionPath)
	if err != nil {
		return nil, fmt.Errorf("account %q parse reversion config: %w", acc.ID, err)
	}
	accCfg.Reversion = MergeReversionConfig(commonReversion, accRevCfg, acc.Exchange)

	obfPath, err := getRequiredAccountConfigPath(acc, "obfuscator", baseDir...)
	if err != nil {
		return nil, err
	}
	obf, err := LoadAndValidate[ObfuscatorConfig](obfPath)
	if err != nil {
		return nil, fmt.Errorf("account %q parse obfuscator config: %w", acc.ID, err)
	}
	accCfg.Obfuscator = obf

	dilPath, err := getRequiredAccountConfigPath(acc, "dilution", baseDir...)
	if err != nil {
		return nil, err
	}
	dil, err := LoadAndValidate[DilutionConfig](dilPath)
	if err != nil {
		return nil, fmt.Errorf("account %q parse dilution config: %w", acc.ID, err)
	}
	accCfg.Dilution = dil

	fundingPath, err := getRequiredAccountConfigPath(acc, "funding", baseDir...)
	if err != nil {
		return nil, err
	}
	symCfgs, err := LoadAndValidate[FundingConfig](fundingPath)
	if err != nil {
		return nil, fmt.Errorf("account %q parse funding config: %w", acc.ID, err)
	}
	accCfg.Symbols = []SymbolConfig(*symCfgs)
	for j := range accCfg.Symbols {
		accCfg.Symbols[j].AccountID = acc.ID
		if accCfg.Symbols[j].Exchange == "" {
			accCfg.Symbols[j].Exchange = acc.Exchange
		}
	}

	return accCfg, nil
}

func applyReversionDefaults(r *ReversionConfig) {
	if r == nil {
		return
	}
	if r.Default.MaxCandidateTrade <= 0 {
		r.Default.MaxCandidateTrade = 1
	}
	for name := range r.Exchanges {
		exch := r.Exchanges[name]
		if exch.MaxCandidateTrade <= 0 {
			exch.MaxCandidateTrade = r.Default.MaxCandidateTrade
			r.Exchanges[name] = exch
		}
	}

	if r.Sync.FundingSync <= 0 {
		r.Sync.FundingSync = types.Duration(30 * time.Second)
	}
	if r.Sync.Time <= 0 {
		r.Sync.Time = types.Duration(30 * time.Second)
	}
	if r.Sync.Ticker <= 0 {
		r.Sync.Ticker = types.Duration(30 * time.Second)
	}
	if r.Sync.Contract <= 0 {
		r.Sync.Contract = types.Duration(300 * time.Second)
	}

	r.TradeSide = strings.ToLower(strings.TrimSpace(r.TradeSide))
	if r.TradeSide == "" {
		r.TradeSide = "both"
	}

	// Normalize Safety limit percentage (guard against repeated division via normalized flag)
	if !r.Safety.normalized {
		if r.Safety.MaxImpactRatio > 1 {
			r.Safety.MaxImpactRatio /= 100
		}
		r.Safety.normalized = true
	}
}

// MergeReversionConfig merges an account-level reversion override over a base common reversion config.
func MergeReversionConfig(base *ReversionConfig, acc *AccountReversionConfig, accountExchange ...string) *ReversionConfig {
	res := cloneBaseReversionConfig(base)
	if acc == nil {
		applyReversionDefaults(res)
		return res
	}

	mergeExchangeReversionConfig(&res.Default, &acc.ExchangeReversionConfig)
	mergeAccountExchangeOverride(res, acc, accountExchange...)

	applyReversionDefaults(res)
	return res
}

func cloneBaseReversionConfig(base *ReversionConfig) *ReversionConfig {
	res := &ReversionConfig{}
	if base == nil {
		res.Exchanges = make(map[string]ExchangeReversionConfig)
		return res
	}
	*res = *base
	if base.Exchanges != nil {
		res.Exchanges = make(map[string]ExchangeReversionConfig, len(base.Exchanges))
		maps.Copy(res.Exchanges, base.Exchanges)
	} else {
		res.Exchanges = make(map[string]ExchangeReversionConfig)
	}
	return res
}

func mergeAccountExchangeOverride(res *ReversionConfig, acc *AccountReversionConfig, accountExchange ...string) {
	if len(accountExchange) == 0 || strings.TrimSpace(accountExchange[0]) == "" {
		return
	}
	rawExch := strings.TrimSpace(accountExchange[0])
	exchangesToUpdate := []string{rawExch}
	baseExch := strings.TrimSuffix(strings.TrimSuffix(rawExch, "_spot"), "_futures")
	if baseExch != rawExch {
		exchangesToUpdate = append(exchangesToUpdate, baseExch)
	}
	futuresExch := baseExch + "_futures"
	if futuresExch != rawExch {
		exchangesToUpdate = append(exchangesToUpdate, futuresExch)
	}

	for _, exch := range exchangesToUpdate {
		cfg, ok := res.Exchanges[exch]
		if !ok {
			cfg = res.Default
		} else {
			mergeExchangeReversionConfig(&cfg, &acc.ExchangeReversionConfig)
		}
		res.Exchanges[exch] = cfg
	}
}

func mergeExchangeReversionConfig(dst, src *ExchangeReversionConfig) {
	if dst != nil && src != nil {
		MergeExchangeReversionConfig(dst, *src)
	}
}

// ReversionForAccount returns the reversion config for the specified account ID,
// or an error if the account is not found or its reversion config is missing.
func (c *Config) ReversionForAccount(accountID string) (*ReversionConfig, error) {
	if c == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if strings.TrimSpace(accountID) == "" {
		return nil, fmt.Errorf("missing required accountID")
	}
	acc, ok := c.Accounts[accountID]
	if !ok || acc == nil {
		return nil, fmt.Errorf("account %q not found in config", accountID)
	}
	if acc.Reversion == nil {
		return nil, fmt.Errorf("account %q missing reversion config", accountID)
	}
	return acc.Reversion, nil
}

// ObfuscatorForAccount returns the obfuscator config for the specified account ID,
// or an error if the account is not found or its obfuscator config is missing.
func (c *Config) ObfuscatorForAccount(accountID string) (*ObfuscatorConfig, error) {
	if c == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if strings.TrimSpace(accountID) == "" {
		return nil, fmt.Errorf("missing required accountID")
	}
	acc, ok := c.Accounts[accountID]
	if !ok || acc == nil {
		return nil, fmt.Errorf("account %q not found in config", accountID)
	}
	if acc.Obfuscator == nil {
		return nil, fmt.Errorf("account %q missing obfuscator config", accountID)
	}
	return acc.Obfuscator, nil
}

// DilutionForAccount returns the dilution config for the specified account ID,
// or an error if the account is not found or its dilution config is missing.
func (c *Config) DilutionForAccount(accountID string) (*DilutionConfig, error) {
	if c == nil {
		return nil, fmt.Errorf("config is nil")
	}
	if strings.TrimSpace(accountID) == "" {
		return nil, fmt.Errorf("missing required accountID")
	}
	acc, ok := c.Accounts[accountID]
	if !ok || acc == nil {
		return nil, fmt.Errorf("account %q not found in config", accountID)
	}
	if acc.Dilution == nil {
		return nil, fmt.Errorf("account %q missing dilution config", accountID)
	}
	return acc.Dilution, nil
}

func (c *Config) validate() error {
	if err := c.validateAccounts(); err != nil {
		return err
	}

	validate := newValidator()

	if err := c.validateAccountSymbols(validate); err != nil {
		return err
	}

	if err := c.validateAccountReversions(validate); err != nil {
		return err
	}

	if err := validate.Struct(c); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	return nil
}

func (c *Config) validateAccounts() error {
	if len(c.Accounts) == 0 {
		return fmt.Errorf("at least one account must be configured")
	}
	for accID, acc := range c.Accounts {
		if strings.TrimSpace(accID) == "" {
			return fmt.Errorf("account ID cannot be empty")
		}
		if acc == nil {
			return fmt.Errorf("account %q config cannot be nil", accID)
		}
		if strings.TrimSpace(acc.Account.ID) == "" {
			return fmt.Errorf("account %q: account.id is required", accID)
		}
	}
	return nil
}

func (c *Config) validateAccountSymbols(validate *validator.Validate) error {
	for accID, acc := range c.Accounts {
		if acc == nil || acc.Reversion == nil {
			continue
		}
		defaults := acc.Reversion.RawFundingReversionConfig
		for i := range acc.Symbols {
			sc := &acc.Symbols[i]
			if sc.AccountID == "" {
				sc.AccountID = accID
			}
			sc.Exchange = strings.ToLower(strings.TrimSpace(sc.Exchange))
			if sc.Exchange == "" {
				return fmt.Errorf("account %q: symbols[%d].exchange is required", accID, i)
			}
			if !c.exchangeConfigured(sc.Exchange) {
				return fmt.Errorf("account %q: symbols[%d].exchange %q is not configured", accID, i, sc.Exchange)
			}
			c.applyAccountDefaults(sc, &defaults, acc.Reversion)
			c.normalizeSymbolMetrics(sc)
			c.defaultSymbolModes(sc)
			if err := validate.Struct(sc); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Config) validateAccountReversions(validate *validator.Validate) error {
	for accID, acc := range c.Accounts {
		if acc != nil && acc.Reversion != nil {
			if err := validate.Struct(acc.Reversion); err != nil {
				return fmt.Errorf("account %q reversion validation failed: %w", accID, err)
			}
		}
	}
	return nil
}

func (c *Config) exchangeConfigured(name string) bool {
	name = strings.ToLower(strings.TrimSpace(name))
	if cfg, ok := c.System.ExchangeConfig[name]; ok {
		return cfg.IsEnabled()
	}
	baseName := strings.TrimSuffix(strings.TrimSuffix(name, "_spot"), "_futures")
	if apiCfg, ok := c.System.ExchangeConfig[baseName]; ok {
		return apiCfg.IsEnabled()
	}
	return false
}

func newValidator() *validator.Validate {
	validate := validator.New()
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name, _, _ := strings.Cut(fld.Tag.Get("json"), ",")
		if name == "-" {
			return ""
		}
		return name
	})
	_ = validate.RegisterValidation("api_config", sysconfig.ValidateAPIConfigField)
	_ = validate.RegisterValidation("supported_exchange", func(fl validator.FieldLevel) bool {
		return sysconfig.IsSupportedExchange(fl.Field().String())
	})
	return validate
}

func MergeExchangeReversionConfig(dest *ExchangeReversionConfig, src ExchangeReversionConfig) {
	mergeReversionCore(dest, src)
	mergeRiskAndScoring(dest, src)
	mergePnLTrailing(dest, src)
	mergeDynamicTP(dest, src)
}

func mergeReversionCore(dest *ExchangeReversionConfig, src ExchangeReversionConfig) {
	if src.TakeProfitPct > 0 {
		dest.TakeProfitPct = src.TakeProfitPct
	}
	if src.StopLossPct > 0 {
		dest.StopLossPct = src.StopLossPct
	}
	if src.BufferTime != 0 {
		dest.BufferTime = src.BufferTime
	}
	if src.PostSettleTimeout != 0 {
		dest.PostSettleTimeout = src.PostSettleTimeout
	}
	if src.Leverage > 0 {
		dest.Leverage = src.Leverage
	}
	if src.MarginUSD > 0 {
		dest.MarginUSD = src.MarginUSD
	}
	if src.MinVol24USD > 0 {
		dest.MinVol24USD = src.MinVol24USD
	}
	if src.MinFundingRate > 0 {
		dest.MinFundingRate = src.MinFundingRate
	}
}

func mergeRiskAndScoring(dest *ExchangeReversionConfig, src ExchangeReversionConfig) {
	if src.MaxCandidateTrade > 0 {
		dest.MaxCandidateTrade = src.MaxCandidateTrade
	}
	if src.MaxMarginUSDOfCandidate > 0 {
		dest.MaxMarginUSDOfCandidate = src.MaxMarginUSDOfCandidate
	}
	if src.ScoringRateWeight > 0 {
		dest.ScoringRateWeight = src.ScoringRateWeight
	}
	if src.ScoringVolumeWeight > 0 {
		dest.ScoringVolumeWeight = src.ScoringVolumeWeight
	}
	if src.MaxVolumeScore > 0 {
		dest.MaxVolumeScore = src.MaxVolumeScore
	}
}

func mergePnLTrailing(dest *ExchangeReversionConfig, src ExchangeReversionConfig) {
	if src.PnLTrailing.Enabled {
		dest.PnLTrailing.Enabled = src.PnLTrailing.Enabled
	}
	if src.PnLTrailing.DropPct > 0 {
		dest.PnLTrailing.DropPct = src.PnLTrailing.DropPct
	}
	if src.PnLTrailing.ConfirmTicks > 0 {
		dest.PnLTrailing.ConfirmTicks = src.PnLTrailing.ConfirmTicks
	}
}

func mergeDynamicTP(dest *ExchangeReversionConfig, src ExchangeReversionConfig) {
	if src.DynamicTP.Enabled {
		dest.DynamicTP.Enabled = src.DynamicTP.Enabled
	}
	if src.DynamicTP.TPMultiplier > 0 {
		dest.DynamicTP.TPMultiplier = src.DynamicTP.TPMultiplier
	}
	if src.DynamicTP.MinTakeProfitPct > 0 {
		dest.DynamicTP.MinTakeProfitPct = src.DynamicTP.MinTakeProfitPct
	}
	if src.DynamicTP.MaxTakeProfitPct > 0 {
		dest.DynamicTP.MaxTakeProfitPct = src.DynamicTP.MaxTakeProfitPct
	}
}

func (c *Config) applyAccountDefaults(sc *SymbolConfig, d *RawFundingReversionConfig, revCfg *ReversionConfig) {
	// Merge exchange-specific configs with strategy defaults.
	exchName := sc.Exchange
	exchConfig := d.Default // Start with defaults

	// Override with exchange-specific settings if present
	if specific, exists := d.Exchanges[exchName]; exists {
		MergeExchangeReversionConfig(&exchConfig, specific)
	}

	if sc.MaxPriceDiffPercent == 0 && revCfg != nil {
		sc.MaxPriceDiffPercent = revCfg.Safety.MaxPriceDiffPercent
	}
	if sc.MinFundingRate == 0 {
		sc.MinFundingRate = exchConfig.MinFundingRate
	}
	if sc.MinVol24USD == 0 {
		sc.MinVol24USD = exchConfig.MinVol24USD
	}

	// Apply leverage and margin (either exchange-specific, or default fallback)
	if sc.Leverage == 0 {
		sc.Leverage = exchConfig.Leverage
	}
	if sc.MarginUSDT == 0 {
		sc.MarginUSDT = exchConfig.MarginUSD
	}

	if sc.OpenType == "" {
		sc.OpenType = OpenType(d.OpenType)
	}
	if sc.PositionMode == "" {
		sc.PositionMode = PositionMode(d.PositionMode)
	}

	c.mergeFundingReversion(sc, d, &exchConfig, revCfg)
}

func (c *Config) mergeFundingReversion(sc *SymbolConfig, d *RawFundingReversionConfig, exchConfig *ExchangeReversionConfig, revCfg *ReversionConfig) {
	var maxLatency types.Duration
	if revCfg != nil {
		maxLatency = revCfg.Safety.MaxLatency
	}

	if !sc.FundingReversion.Enabled && d.Enabled {
		sc.FundingReversion.Enabled = true
		sc.FundingReversion.MaxLatency = maxLatency
		sc.FundingReversion.TakeProfitPct = exchConfig.TakeProfitPct
		sc.FundingReversion.StopLossPct = exchConfig.StopLossPct
		sc.FundingReversion.BufferTime = exchConfig.BufferTime
		sc.FundingReversion.PostSettleTimeout = exchConfig.PostSettleTimeout
		sc.FundingReversion.PnLTrailing = exchConfig.PnLTrailing
		sc.FundingReversion.DynamicTP = exchConfig.DynamicTP
	} else if sc.FundingReversion.Enabled {
		if sc.FundingReversion.MaxLatency == 0 {
			sc.FundingReversion.MaxLatency = maxLatency
		}
		if sc.FundingReversion.TakeProfitPct == 0 {
			sc.FundingReversion.TakeProfitPct = exchConfig.TakeProfitPct
		}
		if sc.FundingReversion.StopLossPct == 0 {
			sc.FundingReversion.StopLossPct = exchConfig.StopLossPct
		}
		if sc.FundingReversion.BufferTime == 0 {
			sc.FundingReversion.BufferTime = exchConfig.BufferTime
		}
		mergeSymbolPnLTrailing(sc, exchConfig)
		mergeSymbolDynamicTP(sc, exchConfig)
	}
}

func mergeSymbolPnLTrailing(sc *SymbolConfig, exchConfig *ExchangeReversionConfig) {
	if !sc.FundingReversion.PnLTrailing.Enabled && exchConfig.PnLTrailing.Enabled {
		sc.FundingReversion.PnLTrailing.Enabled = exchConfig.PnLTrailing.Enabled
	}
	if sc.FundingReversion.PnLTrailing.DropPct == 0 {
		sc.FundingReversion.PnLTrailing.DropPct = exchConfig.PnLTrailing.DropPct
	}
	if sc.FundingReversion.PnLTrailing.ConfirmTicks == 0 {
		sc.FundingReversion.PnLTrailing.ConfirmTicks = exchConfig.PnLTrailing.ConfirmTicks
	}
}

func mergeSymbolDynamicTP(sc *SymbolConfig, exchConfig *ExchangeReversionConfig) {
	if !sc.FundingReversion.DynamicTP.Enabled && exchConfig.DynamicTP.Enabled {
		sc.FundingReversion.DynamicTP.Enabled = exchConfig.DynamicTP.Enabled
	}
	if sc.FundingReversion.DynamicTP.TPMultiplier == 0 {
		sc.FundingReversion.DynamicTP.TPMultiplier = exchConfig.DynamicTP.TPMultiplier
	}
	if sc.FundingReversion.DynamicTP.MinTakeProfitPct == 0 {
		sc.FundingReversion.DynamicTP.MinTakeProfitPct = exchConfig.DynamicTP.MinTakeProfitPct
	}
	if sc.FundingReversion.DynamicTP.MaxTakeProfitPct == 0 {
		sc.FundingReversion.DynamicTP.MaxTakeProfitPct = exchConfig.DynamicTP.MaxTakeProfitPct
	}
}

func (c *Config) normalizeSymbolMetrics(sc *SymbolConfig) {
	sc.MinFundingRate = normalizeFundingRateThreshold(sc.MinFundingRate)

	if sc.FundingReversion.Enabled {
		if sc.FundingReversion.MaxLatency == 0 {
			sc.FundingReversion.MaxLatency = types.Duration(200 * time.Millisecond)
		}
		if sc.FundingReversion.PostSettleTimeout == 0 {
			sc.FundingReversion.PostSettleTimeout = types.Duration(60 * time.Second)
		}

		if sc.FundingReversion.TakeProfitPct <= 0 {
			sc.FundingReversion.TakeProfitPct = 20
		}
		if sc.FundingReversion.StopLossPct <= 0 {
			sc.FundingReversion.StopLossPct = 5
		}
		sc.FundingReversion.TakeProfitPct = normalizePercentRatio(sc.FundingReversion.TakeProfitPct)
		sc.FundingReversion.StopLossPct = normalizePercentRatio(sc.FundingReversion.StopLossPct)
		if sc.FundingReversion.DynamicTP.MinTakeProfitPct > 0 {
			sc.FundingReversion.DynamicTP.MinTakeProfitPct = normalizePercentRatio(sc.FundingReversion.DynamicTP.MinTakeProfitPct)
		}
		if sc.FundingReversion.DynamicTP.MaxTakeProfitPct > 0 {
			sc.FundingReversion.DynamicTP.MaxTakeProfitPct = normalizePercentRatio(sc.FundingReversion.DynamicTP.MaxTakeProfitPct)
		}
	}
}

func (c *Config) defaultSymbolModes(sc *SymbolConfig) {
	switch strings.ToUpper(string(sc.OpenType)) {
	case string(OpenTypeIsolated):
		sc.ParsedOpenType = 1
	case "CROSS":
		sc.ParsedOpenType = 2
	default:
		sc.ParsedOpenType = 1
	}

	switch strings.ToUpper(string(sc.PositionMode)) {
	case string(PositionModeHedge):
		sc.ParsedPositionMode = 1
	case "ONE_WAY":
		sc.ParsedPositionMode = 2
	default:
		sc.ParsedPositionMode = 1
	}
}

// normalizePercentRatio converts user-facing percent values into internal
// ratios. Values already in ratio form are preserved for compatibility.
func normalizePercentRatio(v float64) float64 {
	if v <= 0 {
		return v
	}
	if v <= 0.2 {
		return v
	}
	return v / 100
}

// normalizeFundingRateThreshold accepts either 0.3 for 0.3% or 0.003 for
// 0.3%, then stores the internal exchange-style ratio.
func normalizeFundingRateThreshold(v float64) float64 {
	if v <= 0 {
		return v
	}
	if v <= 0.05 {
		return v
	}
	return v / 100
}

// NewAccountSymbolConfig creates a new SymbolConfig for a specific account and resolves its strategy defaults.
func (c *Config) NewAccountSymbolConfig(accountID, exchangeName, symbol string) (SymbolConfig, error) {
	if c == nil {
		return SymbolConfig{}, fmt.Errorf("config is nil")
	}
	revCfg, err := c.ReversionForAccount(accountID)
	if err != nil {
		return SymbolConfig{}, fmt.Errorf("account %q: %w", accountID, err)
	}

	defaults := revCfg.RawFundingReversionConfig
	sc := SymbolConfig{
		AccountID: accountID,
		Symbol:    symbol,
		Exchange:  exchangeName,
	}

	c.applyAccountDefaults(&sc, &defaults, revCfg)
	c.normalizeSymbolMetrics(&sc)
	c.defaultSymbolModes(&sc)

	return sc, nil
}
