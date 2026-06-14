package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the connection details for the Jellyfin server.
type Config struct {
	Server   string `yaml:"server"`
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

// Load reads a YAML file, parses it, and checks the required fields.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// server and username are mandatory; without them nothing can connect.
	if c.Server == "" || c.Username == "" {
		return nil, fmt.Errorf("incomplete config: 'server' and 'username' are required")
	}
	// Drop a trailing slash so appending paths stays consistent.
	c.Server = strings.TrimRight(c.Server, "/")

	return &c, nil
}
