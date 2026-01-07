package backend

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type BackendsConfig struct {
	Backends []BackendConfig `yaml:"backends"`
	Default  string          `yaml:"default"`
}

type BackendConfig struct {
	Name      string   `yaml:"name"`
	Type      string   `yaml:"type"`
	APIKeyEnv string   `yaml:"api_key_env"`
	APIKey    string   `yaml:"api_key"`
	BaseURL   string   `yaml:"base_url"`
	Addresses []string `yaml:"addresses"`
	Models    []string `yaml:"models"`
}

func LoadConfig(path string) (*BackendsConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config file: %w", err)
	}

	// Expand environment variables in the config
	expanded := os.ExpandEnv(string(data))

	var cfg BackendsConfig
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config file: %w", err)
	}

	return &cfg, nil
}

func LoadRegistry(path string) (*Registry, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}

	return BuildRegistry(cfg)
}

func BuildRegistry(cfg *BackendsConfig) (*Registry, error) {
	registry := NewRegistry()

	for _, bc := range cfg.Backends {
		apiKey := bc.APIKey
		if bc.APIKeyEnv != "" {
			apiKey = os.Getenv(bc.APIKeyEnv)
		}

		var b Backend
		var err error

		switch bc.Type {
		case "openai":
			b = NewOpenAIBackend(OpenAIConfig{
				Name:    bc.Name,
				APIKey:  apiKey,
				BaseURL: bc.BaseURL,
				Models:  bc.Models,
			})

		case "anthropic":
			b = NewAnthropicBackend(AnthropicConfig{
				Name:    bc.Name,
				APIKey:  apiKey,
				BaseURL: bc.BaseURL,
				Models:  bc.Models,
			})

		case "grpc":
			b, err = NewGRPCBackend(GRPCConfig{
				Name:      bc.Name,
				Addresses: bc.Addresses,
				Models:    bc.Models,
			})
			if err != nil {
				return nil, fmt.Errorf("create grpc backend %s: %w", bc.Name, err)
			}

		default:
			return nil, fmt.Errorf("unknown backend type: %s", bc.Type)
		}

		registry.Register(b)
	}

	if cfg.Default != "" {
		if err := registry.SetDefault(cfg.Default); err != nil {
			return nil, err
		}
	}

	return registry, nil
}
