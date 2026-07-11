// Package config loads the restic-reporter YAML configuration that describes
// each backup job and where to publish its metrics.
package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration loaded from a YAML file.
type Config struct {
	Restic ResticConfig `yaml:"restic"`
	MQTT   MQTTConfig   `yaml:"mqtt"`
	// StateDir holds per-job last-success timestamp files.
	StateDir string `yaml:"state_dir"`
	// LogFile receives restic output and diagnostics; empty means stderr only.
	LogFile string `yaml:"log_file"`
	Jobs    []Job  `yaml:"jobs"`
}

// ResticConfig holds settings shared by every restic invocation.
type ResticConfig struct {
	// PasswordFile is passed to restic as --password-file. The password is
	// never read or stored by restic-reporter itself.
	PasswordFile string `yaml:"password_file"`
	// Binary is the restic executable to run (default "restic" on PATH).
	Binary string `yaml:"binary"`
}

// MQTTConfig describes the broker and the topic layout. It intentionally
// mirrors the topics/prefixes used by the original restic-backup.sh so that
// existing Home Assistant entities keep working unchanged.
type MQTTConfig struct {
	Host            string `yaml:"host"`
	Port            int    `yaml:"port"`
	Username        string `yaml:"username"`
	Password        string `yaml:"password"`
	DiscoveryPrefix string `yaml:"discovery_prefix"`
	TopicPrefix     string `yaml:"topic_prefix"`
}

// Job is a single restic backup: a repo and the source path to snapshot.
type Job struct {
	// ID is the machine-readable job key (e.g. "minecraft"). It becomes part
	// of MQTT topics and Home Assistant unique_ids, so changing it orphans the
	// existing HA entities.
	ID string `yaml:"id"`
	// Name is the human-readable label shown in Home Assistant.
	Name   string `yaml:"name"`
	Repo   string `yaml:"repo"`
	Source string `yaml:"source"`
}

// Load reads, parses, and validates the config at path.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.Restic.Binary == "" {
		c.Restic.Binary = "restic"
	}
	if c.MQTT.Port == 0 {
		c.MQTT.Port = 1883
	}
	if c.MQTT.DiscoveryPrefix == "" {
		c.MQTT.DiscoveryPrefix = "homeassistant"
	}
}

func (c *Config) validate() error {
	if c.Restic.PasswordFile == "" {
		return fmt.Errorf("restic.password_file is required")
	}
	if c.MQTT.Host == "" {
		return fmt.Errorf("mqtt.host is required")
	}
	if c.MQTT.TopicPrefix == "" {
		return fmt.Errorf("mqtt.topic_prefix is required")
	}
	if c.StateDir == "" {
		return fmt.Errorf("state_dir is required")
	}
	if len(c.Jobs) == 0 {
		return fmt.Errorf("at least one job is required")
	}
	seen := map[string]bool{}
	for i, j := range c.Jobs {
		switch {
		case j.ID == "":
			return fmt.Errorf("jobs[%d]: id is required", i)
		case j.Repo == "":
			return fmt.Errorf("job %q: repo is required", j.ID)
		case j.Source == "":
			return fmt.Errorf("job %q: source is required", j.ID)
		case seen[j.ID]:
			return fmt.Errorf("duplicate job id %q", j.ID)
		}
		seen[j.ID] = true
	}
	return nil
}
