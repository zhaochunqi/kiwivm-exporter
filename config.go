package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type NodeConfig struct {
	Veid   int64  `yaml:"veid"`
	APIKey string `yaml:"api_key"`
}

type Config struct {
	Endpoint string       `yaml:"endpoint"`
	Nodes    []NodeConfig `yaml:"nodes"`
}

func LoadConfig(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if c.Endpoint == "" {
		c.Endpoint = "https://api.64clouds.com/v1"
	}
	if len(c.Nodes) == 0 {
		return nil, fmt.Errorf("%s: no nodes configured", path)
	}
	for _, n := range c.Nodes {
		if n.Veid == 0 || n.APIKey == "" {
			return nil, fmt.Errorf("%s: node missing veid or api_key", path)
		}
	}
	return &c, nil
}
