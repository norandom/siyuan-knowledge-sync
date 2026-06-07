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

	// Ontology is the optional `ontology:` section that lets operators override
	// the compile-time default schema (domains, intents, canonical folders,
	// controlled tag vocabulary). A nil pointer means the section was absent
	// and main.go must skip ontology.Configure() so the compile-time defaults
	// stay in effect (Requirement 1.2). LoadConfig only decodes this field;
	// the full validation (charset, duplicates, reserved prefixes) is the
	// responsibility of ontology.Configure() called from main.go.
	Ontology *OntologyConfig `yaml:"ontology,omitempty"`
}

// OntologyConfig is the decode target for the optional `ontology:` section
// of .siyuan-sync.yaml. When this struct is reachable through a non-nil
// Config.Ontology pointer, main.go translates it into ontology.ConfigureOptions
// and calls ontology.Configure() exactly once before any subcommand runs.
type OntologyConfig struct {
	Domains []OntologyDomain `yaml:"domains"`
	Intents []OntologyIntent `yaml:"intents"`
	// Tags is the optional controlled tag vocabulary. A nil slice means the
	// `tags:` key was absent (open vocabulary stays in effect). A non-nil
	// empty slice (`tags: []`) means an explicit empty vocabulary that
	// rejects every tag. The nil-vs-non-nil-empty distinction must survive
	// the YAML decode so Task 4.1 can translate it into ConfigureOptions.Tags.
	Tags []string `yaml:"tags,omitempty"`
}

// OntologyDomain pairs a domain identifier with its canonical folder name.
// Validation (id charset, folder reserved-prefix, duplicates) happens later
// in ontology.Configure(); LoadConfig only decodes.
type OntologyDomain struct {
	ID     string `yaml:"id"`
	Folder string `yaml:"folder"`
}

// OntologyIntent carries a single intent identifier. Validation (charset,
// duplicates) happens later in ontology.Configure(); LoadConfig only decodes.
type OntologyIntent struct {
	ID string `yaml:"id"`
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
