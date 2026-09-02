package config

import (
	"fmt"
	"os"
	"strings"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	sysconfig "crypto-bot/internal/infrastructure/config"
	pkgconfig "crypto-bot/pkg/config"
)

// SystemConfig wraps infrastructure system and exchange configuration.
type SystemConfig struct {
	sysconfig.SystemConfig
}

// BlacklistConfig holds blacklisted symbols mapped by exchange variant or common.
type BlacklistConfig map[string][]string

// GetAllSymbols returns all blacklisted symbols across all sections.
func (b BlacklistConfig) GetAllSymbols() []string {
	out := make([]string, 0)
	seen := make(map[string]bool)
	for _, list := range b {
		for _, sym := range list {
			if !seen[sym] {
				seen[sym] = true
				out = append(out, sym)
			}
		}
	}
	return out
}

// LoadSystemConfig loads system.jsonc and exchange.jsonc.
func LoadSystemConfig(systemPath, exchangePath string) (*SystemConfig, error) {
	sysRaw, err := pkgconfig.Load[sysconfig.SystemConfig](systemPath)
	if err != nil {
		return nil, fmt.Errorf("load system config: %w", err)
	}

	exchRaw, err := pkgconfig.Load[sysconfig.SystemConfig](exchangePath)
	if err != nil {
		return nil, fmt.Errorf("load exchange config: %w", err)
	}
	sysRaw.ExchangeConfig = exchRaw.ExchangeConfig

	// Penny Jumper operates on public data streams and does not require private API credentials.
	// Populate placeholder credentials for any enabled exchanges where credentials were not supplied.
	for exch, apiCfg := range sysRaw.ExchangeConfig {
		if apiCfg.APIKey == "" {
			apiCfg.APIKey = "public_key"
		}
		if apiCfg.APISecret == "" {
			apiCfg.APISecret = "public_secret"
		}
		if spec := sysconfig.ExchangeSpecs[exch]; spec.RequiresPassphrase && apiCfg.APIPassphrase == "" {
			apiCfg.APIPassphrase = "public_passphrase"
		}
		sysRaw.ExchangeConfig[exch] = apiCfg
	}

	if err := sysconfig.InitializeBase(sysRaw); err != nil {
		return nil, fmt.Errorf("initialize base config: %w", err)
	}

	return &SystemConfig{SystemConfig: *sysRaw}, nil
}

// LoadPennyJumperConfig loads penny_jumper.jsonc and blacklist.jsonc.
func LoadPennyJumperConfig(botPath, blacklistPath string) (*pjdomain.PennyJumperConfig, []string, error) {
	cfg, err := pkgconfig.Load[pjdomain.PennyJumperConfig](botPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load penny jumper config: %w", err)
	}
	cfg.ApplyDefaults()

	if err := injectAIProxyCredentials(cfg); err != nil {
		return nil, nil, fmt.Errorf("ai proxy credentials: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, nil, fmt.Errorf("validate penny jumper config: %w", err)
	}

	var blacklist []string
	blk, err := pkgconfig.Load[BlacklistConfig](blacklistPath)
	if err == nil && blk != nil {
		blacklist = blk.GetAllSymbols()
	}

	return cfg, blacklist, nil
}

func injectAIProxyCredentials(cfg *pjdomain.PennyJumperConfig) error {
	if cfg.WallJudge.Mode != pjdomain.WallJudgeModeModel && cfg.WallJudge.Mode != pjdomain.WallJudgeModeDual {
		return nil
	}

	injectFromEnv(&cfg.WallJudge)
	return fallbackFromBitwarden(&cfg.WallJudge)
}

func injectFromEnv(wj *pjdomain.WallJudgeConfig) {
	if url := strings.TrimSpace(os.Getenv("AI_PROXY_URL")); url != "" {
		wj.Endpoint = url
	}
	if key := strings.TrimSpace(os.Getenv("AI_PROXY_API_KEY")); key != "" {
		wj.ApiKey = key
	}
	if model := strings.TrimSpace(os.Getenv("AI_PROXY_MODEL")); model != "" {
		wj.ModelName = model
	}
}

func fallbackFromBitwarden(wj *pjdomain.WallJudgeConfig) error {
	if (wj.Endpoint != "" && wj.ApiKey != "" && wj.ModelName != "") || !sysconfig.HasBitwardenConfig() {
		return nil
	}

	loader, err := sysconfig.NewBitwardenLoader()
	if err != nil {
		return fmt.Errorf("bitwarden fallback failed: %w", err)
	}

	fetchSecret := func(target *string, key string) {
		if *target == "" {
			if val, err := loader.GetSecret(key); err == nil && val != "" {
				*target = strings.TrimSpace(val)
			}
		}
	}

	fetchSecret(&wj.Endpoint, "AI_PROXY_URL")
	fetchSecret(&wj.ApiKey, "AI_PROXY_API_KEY")
	fetchSecret(&wj.ModelName, "AI_PROXY_MODEL")
	return nil
}
