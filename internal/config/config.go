// Package config loads and stores pbmac configuration. v1 uses a JSON file
// under the user config dir; credential storage will move to the macOS Keychain
// in milestone M5 (see docs/DESIGN.md §8). Secrets are not written to the JSON
// file today — the API token is read from the environment.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// EnvRepository is the environment variable holding the default repository spec.
const EnvRepository = "PBS_REPOSITORY"

// EnvAPIToken holds the API token "USER@REALM!TOKENID:SECRET".
const EnvAPIToken = "PBS_API_TOKEN"

// EnvFingerprint holds the expected server certificate SHA-256 fingerprint.
const EnvFingerprint = "PBS_FINGERPRINT"

// Config is the persisted, non-secret configuration.
type Config struct {
	Repository  string `json:"repository,omitempty"`
	Fingerprint string `json:"fingerprint,omitempty"`
}

// Path returns the config file path (~/.config/pbmac/config.json or the OS
// equivalent).
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pbmac", "config.json"), nil
}

// Load reads the config file, returning a zero Config if none exists.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save writes the config file, creating parent directories as needed.
func (c *Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// ResolveRepository returns the repository spec from the flag, env, or config,
// in that precedence order.
func (c *Config) ResolveRepository(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if env := os.Getenv(EnvRepository); env != "" {
		return env
	}
	return c.Repository
}
