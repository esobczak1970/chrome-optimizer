// Package config owns Chrome Optimizer's YAML-only application settings.
package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const MaxTimeout = 60 * time.Second

// Config represents the public YAML configuration. Keep transport details out
// of the view layer. A duration is serialized as a YAML string such as "15s".
type Config struct {
	CDPEndpoint string `yaml:"cdp_endpoint"`
	Timeout     string `yaml:"timeout"`
	DefaultSort string `yaml:"default_sort"`
}

func Default() Config {
	return Config{
		CDPEndpoint: "http://127.0.0.1:9222",
		Timeout:     "15s",
		DefaultSort: "domain",
	}
}

// Load accepts exactly one YAML document, rejects unknown fields and validates
// all values before any browser connection is attempted.
func Load(path string) (Config, error) {
	cfg := Default()
	f, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open YAML configuration %q: %w", path, err)
	}
	defer f.Close()

	decoder := yaml.NewDecoder(f)
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		if errors.Is(err, io.EOF) {
			return Config{}, fmt.Errorf("YAML configuration %q is empty", path)
		}
		return Config{}, fmt.Errorf("decode YAML configuration %q: %w", path, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return Config{}, errors.New("YAML configuration must contain exactly one document")
		}
		return Config{}, fmt.Errorf("check YAML documents: %w", err)
	}
	if _, err := cfg.Duration(); err != nil {
		return Config{}, err
	}
	cfg.DefaultSort = strings.ToLower(strings.TrimSpace(cfg.DefaultSort))
	if cfg.DefaultSort != "domain" && cfg.DefaultSort != "title" && cfg.DefaultSort != "id" {
		return Config{}, fmt.Errorf("default_sort %q must be domain, title, or id in phase 1", cfg.DefaultSort)
	}
	if strings.TrimSpace(cfg.CDPEndpoint) == "" {
		return Config{}, errors.New("cdp_endpoint must not be empty")
	}
	return cfg, nil
}

func (c Config) Duration() (time.Duration, error) {
	d, err := time.ParseDuration(strings.TrimSpace(c.Timeout))
	if err != nil || d <= 0 || d > MaxTimeout {
		return 0, fmt.Errorf("timeout %q must be a duration greater than 0 and at most %s", c.Timeout, MaxTimeout)
	}
	return d, nil
}
