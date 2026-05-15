package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Endpoint string `yaml:"endpoint"`
	Token    string `yaml:"token"`
	RepoPath string `yaml:"repo_path"`
	AutoFix  bool   `yaml:"autofix"`

	// Optional Cloudflare Access service-token credentials. When set, they are
	// sent as CF-Access-Client-Id / CF-Access-Client-Secret headers on every
	// SiYuan request so the tool can reach an endpoint protected by Cloudflare
	// Access (Zero Trust). Leave empty when the endpoint is not behind CF Access.
	CFAccessClientID     string `yaml:"cf_access_client_id"`
	CFAccessClientSecret string `yaml:"cf_access_client_secret"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found at %s", path)
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("config file at %s is empty", path)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file at %s: %w", path, err)
	}

	if cfg.Endpoint == "" {
		return nil, fmt.Errorf("missing required field \"endpoint\" in config file %s", path)
	}
	if cfg.Token == "" {
		return nil, fmt.Errorf("missing required field \"token\" in config file %s", path)
	}
	if cfg.RepoPath == "" {
		return nil, fmt.Errorf("missing required field \"repo_path\" in config file %s", path)
	}

	return &cfg, nil
}
