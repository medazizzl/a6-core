package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

type Config struct {
	AppEnv   string `json:"app_env"`
	LogLevel string `json:"log_level"`
	DataDir  string `json:"data_dir"`
}

func DefaultConfig() *Config {
	return &Config{
		AppEnv:   "development",
		LogLevel: "info",
		DataDir:  "./data",
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	if path == "" {
		return cfg, nil
	}

	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}
