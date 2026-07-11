package config

import (
	"os"
	"path/filepath"
	"testing"
)

// sampleYAML mirrors config.example.yaml, including the underscore-separated
// keys (topic_prefix, state_dir, password_file, …) that only decode correctly
// when viper is told to use the struct yaml tags.
const sampleYAML = `
restic:
  password_file: /etc/restic/password
  binary: restic
mqtt:
  host: 192.168.18.131
  port: 1883
  discovery_prefix: homeassistant
  topic_prefix: restic/gaming-server
state_dir: /var/lib/restic-backup
log_file: /tmp/restic-backup.log
jobs:
  - id: minecraft
    name: Minecraft
    repo: sftp:restic-backup@100.64.0.11:/mnt/backups/gaming-server/minecraft
    source: /opt/game-servers/minecraft/data
  - id: vintage_story
    name: Vintage Story
    repo: sftp:restic-backup@100.64.0.11:/mnt/backups/gaming-server/vintage-story
    source: /opt/game-servers/vintagestory/data
`

func writeConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

// TestLoadRoundTrip verifies the viper->struct decode maps every underscore key
// correctly (the silent failure mode when the yaml tag name is not wired).
func TestLoadRoundTrip(t *testing.T) {
	cfg, err := Load(writeConfig(t, sampleYAML))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.Restic.PasswordFile != "/etc/restic/password" {
		t.Errorf("restic.password_file = %q", cfg.Restic.PasswordFile)
	}
	if cfg.MQTT.Host != "192.168.18.131" {
		t.Errorf("mqtt.host = %q", cfg.MQTT.Host)
	}
	if cfg.MQTT.TopicPrefix != "restic/gaming-server" {
		t.Errorf("mqtt.topic_prefix = %q", cfg.MQTT.TopicPrefix)
	}
	if cfg.MQTT.DiscoveryPrefix != "homeassistant" {
		t.Errorf("mqtt.discovery_prefix = %q", cfg.MQTT.DiscoveryPrefix)
	}
	if cfg.StateDir != "/var/lib/restic-backup" {
		t.Errorf("state_dir = %q", cfg.StateDir)
	}
	if cfg.LogFile != "/tmp/restic-backup.log" {
		t.Errorf("log_file = %q", cfg.LogFile)
	}
	if len(cfg.Jobs) != 2 {
		t.Fatalf("len(jobs) = %d, want 2", len(cfg.Jobs))
	}
	if cfg.Jobs[1].ID != "vintage_story" {
		t.Errorf("jobs[1].id = %q", cfg.Jobs[1].ID)
	}
	if cfg.Jobs[1].Source != "/opt/game-servers/vintagestory/data" {
		t.Errorf("jobs[1].source = %q", cfg.Jobs[1].Source)
	}
}

// TestLoadDefaults confirms viper's defaults fill fields omitted from the file.
func TestLoadDefaults(t *testing.T) {
	const minimal = `
restic:
  password_file: /etc/restic/password
mqtt:
  host: broker.local
  topic_prefix: restic/host
state_dir: /var/lib/restic-backup
jobs:
  - id: only
    name: Only
    repo: sftp:x@host:/repo
    source: /data
`
	cfg, err := Load(writeConfig(t, minimal))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Restic.Binary != "restic" {
		t.Errorf("restic.binary default = %q, want restic", cfg.Restic.Binary)
	}
	if cfg.MQTT.Port != 1883 {
		t.Errorf("mqtt.port default = %d, want 1883", cfg.MQTT.Port)
	}
	if cfg.MQTT.DiscoveryPrefix != "homeassistant" {
		t.Errorf("mqtt.discovery_prefix default = %q, want homeassistant", cfg.MQTT.DiscoveryPrefix)
	}
}

// TestLoadEnvOverride confirms RESTIC_REPORTER_* env vars override file values.
func TestLoadEnvOverride(t *testing.T) {
	t.Setenv("RESTIC_REPORTER_MQTT_HOST", "10.0.0.9")
	t.Setenv("RESTIC_REPORTER_MQTT_PORT", "8883")

	cfg, err := Load(writeConfig(t, sampleYAML))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.MQTT.Host != "10.0.0.9" {
		t.Errorf("env override mqtt.host = %q, want 10.0.0.9", cfg.MQTT.Host)
	}
	if cfg.MQTT.Port != 8883 {
		t.Errorf("env override mqtt.port = %d, want 8883", cfg.MQTT.Port)
	}
}

// TestLoadMissingRequired confirms validation still fires after the viper path.
func TestLoadMissingRequired(t *testing.T) {
	const noHost = `
restic:
  password_file: /etc/restic/password
mqtt:
  topic_prefix: restic/host
state_dir: /var/lib/restic-backup
jobs:
  - id: only
    repo: sftp:x@host:/repo
    source: /data
`
	_, err := Load(writeConfig(t, noHost))
	if err == nil {
		t.Fatal("expected error for missing mqtt.host, got nil")
	}
}
