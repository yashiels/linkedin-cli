// Package config manages lnk CLI configuration loaded from TOML.
package config

import (
	"fmt"
	"github.com/yashiels/linkedin-cli/internal/safety"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

const (
	defaultConfigDir  = ".config/lnk"
	defaultConfigFile = "config.toml"
	envConfigPath     = "LNK_CONFIG"
)

// Config is the top-level configuration structure.
type Config struct {
	Safety   SafetyConfig   `toml:"safety"`
	Defaults DefaultsConfig `toml:"defaults"`
	Display  DisplayConfig  `toml:"display"`
}

// DefaultsConfig holds sensible default values for search and pagination.
type DefaultsConfig struct {
	Location string `toml:"location"`
	Sort     string `toml:"sort"`  // "relevant" or "recent"
	Limit    int    `toml:"limit"` // default 25
}

// DisplayConfig controls terminal output behaviour.
type DisplayConfig struct {
	Color bool `toml:"color"`
}

type SafetyConfig struct {
	DelaySeconds int `toml:"delay_seconds"`
	Budget       int `toml:"budget"`
}

func defaults() Config {
	return Config{
		Safety: SafetyConfig{DelaySeconds: 5, Budget: 100},
		Defaults: DefaultsConfig{
			Sort:  "relevant",
			Limit: 25,
		},
		Display: DisplayConfig{
			Color: true,
		},
	}
}

// Load reads the TOML config file, returning defaults for missing keys.
// Precedence: LNK_CONFIG env var path > ~/.config/lnk/config.toml > built-ins.
func Load() (Config, error) {
	cfg := defaults()
	path, err := ConfigPath()
	if err != nil {
		return cfg, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// No file yet — use defaults silently.
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("config: cannot read configuration")
	}

	metadata, err := toml.Decode(string(data), &cfg)
	if err != nil {
		return cfg, fmt.Errorf("config: cannot parse configuration")
	}

	for _, key := range metadata.Undecoded() {
		if len(key) > 0 && key[0] == "safety" {
			return cfg, fmt.Errorf("config: unknown safety setting")
		}
	}
	if cfg.Safety.DelaySeconds < 5 || cfg.Safety.DelaySeconds > int(safety.MaxDelay/time.Second) || cfg.Safety.Budget < 1 || cfg.Safety.Budget > 100 {
		return cfg, fmt.Errorf("config: safety requires representable delay_seconds >= 5 and budget in [1,100]")
	}
	return cfg, nil
}

// Save writes cfg to the config file path, creating parent directories as needed.
func Save(cfg Config) error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("config: cannot create directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("config: cannot open config file: %w", err)
	}
	defer f.Close()
	if err := toml.NewEncoder(f).Encode(cfg); err != nil {
		return fmt.Errorf("config: cannot encode config: %w", err)
	}
	return nil
}

// ConfigPath returns the resolved configuration file path, honouring LNK_CONFIG.
func ConfigPath() (string, error) {
	if p := os.Getenv(envConfigPath); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: cannot determine home directory: %w", err)
	}
	return filepath.Join(home, defaultConfigDir, defaultConfigFile), nil
}
