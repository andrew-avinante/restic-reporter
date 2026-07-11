// Package config loads the restic-reporter configuration that describes each
// backup job and where to publish its metrics. Configuration is read from a
// YAML file via viper, with optional environment-variable overrides.
package config

import (
	"fmt"
	"strings"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"
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

// envPrefix namespaces environment-variable overrides, e.g.
// RESTIC_REPORTER_MQTT_HOST overrides mqtt.host.
const envPrefix = "RESTIC_REPORTER"

// envKeys are the scalar config keys that may be overridden by environment
// variables. Slice-valued keys (jobs) are intentionally file-only.
var envKeys = []string{
	"restic.password_file",
	"restic.binary",
	"mqtt.host",
	"mqtt.port",
	"mqtt.username",
	"mqtt.password",
	"mqtt.discovery_prefix",
	"mqtt.topic_prefix",
	"state_dir",
	"log_file",
}

// Load reads, parses, and validates the config at path. Environment variables
// prefixed with RESTIC_REPORTER_ override the matching file values.
func Load(path string) (*Config, error) {
	v := newViper()
	v.SetConfigFile(path)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return unmarshal(v)
}

// newViper returns a viper preconfigured with defaults and environment binding
// but no config source attached. Exposed helpers use it so the file and
// in-memory (test) load paths stay identical.
func newViper() *viper.Viper {
	v := viper.New()
	v.SetConfigType("yaml")

	v.SetDefault("restic.binary", "restic")
	v.SetDefault("mqtt.port", 1883)
	v.SetDefault("mqtt.discovery_prefix", "homeassistant")

	v.SetEnvPrefix(envPrefix)
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	// AutomaticEnv alone does not surface un-defaulted keys during Unmarshal,
	// so bind each scalar key explicitly.
	for _, key := range envKeys {
		_ = v.BindEnv(key)
	}
	return v
}

// unmarshal decodes v into a validated Config using the struct yaml tags.
func unmarshal(v *viper.Viper) (*Config, error) {
	var c Config
	if err := v.Unmarshal(&c, func(dc *mapstructure.DecoderConfig) { dc.TagName = "yaml" }); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	c.ApplyDefaults()
	if err := c.Validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

// ApplyDefaults fills any zero-valued fields that have a default. It is
// idempotent and safe to call even after viper has already applied defaults.
func (c *Config) ApplyDefaults() {
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

// Validate reports the first configuration problem, if any.
func (c *Config) Validate() error {
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
