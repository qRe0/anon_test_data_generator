package config

import (
	"fmt"
	"os"

	yaml "gopkg.in/yaml.v3"
)

// Config represents the root YAML configuration.
type Config struct {
	Global GlobalConfig `yaml:"global"`
	Tables []TableRule  `yaml:"tables"`
}

// GlobalConfig holds top-level settings.
type GlobalConfig struct {
	DSN       string `yaml:"dsn"`
	Seed      int64  `yaml:"seed"`
	Locale    string `yaml:"locale"`
	BatchSize int    `yaml:"batch_size"`
}

// TableRule defines generation parameters for a single database table.
type TableRule struct {
	Name            string       `yaml:"name"`
	Count           int          `yaml:"count"`
	CleanupStrategy string       `yaml:"cleanup_strategy"`
	Columns         []ColumnRule `yaml:"columns"`
}

// ColumnRule defines the generator and parameters for a single column.
type ColumnRule struct {
	Name        string         `yaml:"name"`
	Generator   string         `yaml:"generator"`
	Params      map[string]any `yaml:"params"`
	IsUnique    bool           `yaml:"is_unique"`
	Transformer string         `yaml:"transformer"`
}

// Load reads and parses a YAML configuration file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	cfg := &Config{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	applyDefaults(cfg)
	return cfg, nil
}

func applyDefaults(cfg *Config) {
	if cfg.Global.Locale == "" {
		cfg.Global.Locale = "en_US"
	}
	if cfg.Global.BatchSize == 0 {
		cfg.Global.BatchSize = 1000
	}
}
