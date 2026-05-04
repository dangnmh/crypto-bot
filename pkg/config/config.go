package config

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/tailscale/hujson"
)

// Load reads a standard JSON or JSONC file and unmarshals it into a generic type T.
func Load[T any](path string) (*T, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}

	data, err = hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("standardize config json: %w", err)
	}

	var cfg T
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	return &cfg, nil
}
