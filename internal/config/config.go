package config

import "gopkg.in/yaml.v3"

type Config struct {
	Endpoint string `yaml:"endpoint"`
	Token    string `yaml:"token"`
	RepoPath string `yaml:"repo_path"`
	AutoFix  bool   `yaml:"autofix"`
}

var _ = yaml.Marshal
