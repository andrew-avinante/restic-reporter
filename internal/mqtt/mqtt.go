// Package mqtt publishes backup metrics to an MQTT broker using Home Assistant
// MQTT discovery. The topics, unique_ids, device identifiers, and value
// templates are kept byte-compatible with the original restic-backup.sh so the
// existing Home Assistant entities continue to work without reconfiguration.
package mqtt

import (
	"encoding/json"
	"fmt"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

// Config configures the broker connection and topic layout.
type Config struct {
	Host            string
	Port            int
	Username        string
	Password        string
	DiscoveryPrefix string // e.g. "homeassistant"
	TopicPrefix     string // e.g. "restic/gaming-server"
}

// State is the JSON payload published to a job's state topic. The field names
// and units match what the Home Assistant value_templates expect.
type State struct {
	Status              string `json:"status"`
	Timestamp           string `json:"timestamp"`
	DurationSeconds     int64  `json:"duration_seconds"`
	FilesNew            int64  `json:"files_new"`
	FilesChanged        int64  `json:"files_changed"`
	FilesUnmodified     int64  `json:"files_unmodified"`
	DataAddedBytes      int64  `json:"data_added_bytes"`
	TotalFilesProcessed int64  `json:"total_files_processed"`
	TotalBytesProcessed int64  `json:"total_bytes_processed"`
	SnapshotID          string `json:"snapshot_id"`
	LastSuccess         string `json:"last_success"`
	Error               string `json:"error"`
}

// Publisher holds a connected MQTT client.
type Publisher struct {
	cfg    Config
	client paho.Client
}

// Connect dials the broker and returns a ready Publisher. Call Close when done.
func Connect(cfg Config) (*Publisher, error) {
	opts := paho.NewClientOptions().
		AddBroker(fmt.Sprintf("tcp://%s:%d", cfg.Host, cfg.Port)).
		SetClientID("restic-reporter").
		SetConnectTimeout(10 * time.Second).
		SetOrderMatters(false)
	if cfg.Username != "" {
		opts.SetUsername(cfg.Username)
		opts.SetPassword(cfg.Password)
	}

	client := paho.NewClient(opts)
	tok := client.Connect()
	if !tok.WaitTimeout(15 * time.Second) {
		return nil, fmt.Errorf("mqtt connect: timed out connecting to %s:%d", cfg.Host, cfg.Port)
	}
	if err := tok.Error(); err != nil {
		return nil, fmt.Errorf("mqtt connect: %w", err)
	}
	return &Publisher{cfg: cfg, client: client}, nil
}

// Close flushes and disconnects the client.
func (p *Publisher) Close() {
	p.client.Disconnect(500) // ms; allow in-flight publishes to drain
}

// deviceInfo is the shared HA device block for a job's sensors.
type deviceInfo struct {
	Identifiers  []string `json:"identifiers"`
	Name         string   `json:"name"`
	Manufacturer string   `json:"manufacturer"`
	Model        string   `json:"model"`
}

// discoveryConfig is one Home Assistant sensor discovery payload. Optional
// fields use omitempty so each sensor emits only the keys the shell version did.
type discoveryConfig struct {
	Name                string     `json:"name"`
	UniqueID            string     `json:"unique_id"`
	StateTopic          string     `json:"state_topic"`
	ValueTemplate       string     `json:"value_template"`
	JSONAttributesTopic string     `json:"json_attributes_topic,omitempty"`
	UnitOfMeasurement   string     `json:"unit_of_measurement,omitempty"`
	DeviceClass         string     `json:"device_class,omitempty"`
	StateClass          string     `json:"state_class,omitempty"`
	Icon                string     `json:"icon,omitempty"`
	Device              deviceInfo `json:"device"`
}

// discoveryMessage is a single retained discovery config plus its topic.
type discoveryMessage struct {
	Topic  string
	Config discoveryConfig
}

// buildDiscovery constructs the HA discovery messages for a job. It is pure
// (no I/O) so the byte-compatibility with restic-backup.sh can be unit tested.
// jobID/name mirror restic-backup.sh: device_id is "restic_<jobID>".
func buildDiscovery(cfg Config, jobID, name string) []discoveryMessage {
	deviceID := "restic_" + jobID
	stateTopic := fmt.Sprintf("%s/%s/state", cfg.TopicPrefix, jobID)
	dev := deviceInfo{
		Identifiers:  []string{deviceID},
		Name:         "Restic Backup - " + name,
		Manufacturer: "restic",
		Model:        "SFTP backup",
	}

	sensors := []struct {
		key string
		cfg discoveryConfig
	}{
		{"status", discoveryConfig{
			Name:                "Status",
			UniqueID:            deviceID + "_status",
			StateTopic:          stateTopic,
			ValueTemplate:       "{{ value_json.status }}",
			JSONAttributesTopic: stateTopic,
			Icon:                "mdi:backup-restore",
			Device:              dev,
		}},
		{"duration", discoveryConfig{
			Name:              "Backup Duration",
			UniqueID:          deviceID + "_duration",
			StateTopic:        stateTopic,
			ValueTemplate:     "{{ value_json.duration_seconds }}",
			UnitOfMeasurement: "s",
			DeviceClass:       "duration",
			StateClass:        "measurement",
			Device:            dev,
		}},
		{"data_added", discoveryConfig{
			Name:              "Data Added",
			UniqueID:          deviceID + "_data_added",
			StateTopic:        stateTopic,
			ValueTemplate:     "{{ (value_json.data_added_bytes | float / 1048576) | round(2) }}",
			UnitOfMeasurement: "MB",
			StateClass:        "measurement",
			Icon:              "mdi:database-arrow-up",
			Device:            dev,
		}},
		{"last_success", discoveryConfig{
			Name:          "Last Successful Backup",
			UniqueID:      deviceID + "_last_success",
			StateTopic:    stateTopic,
			ValueTemplate: "{{ value_json.last_success }}",
			DeviceClass:   "timestamp",
			Device:        dev,
		}},
	}

	msgs := make([]discoveryMessage, len(sensors))
	for i, s := range sensors {
		msgs[i] = discoveryMessage{
			Topic:  fmt.Sprintf("%s/sensor/%s/%s/config", cfg.DiscoveryPrefix, deviceID, s.key),
			Config: s.cfg,
		}
	}
	return msgs
}

// PublishDiscovery publishes the retained HA discovery configs for a job.
func (p *Publisher) PublishDiscovery(jobID, name string) error {
	for _, m := range buildDiscovery(p.cfg, jobID, name) {
		payload, err := json.Marshal(m.Config)
		if err != nil {
			return fmt.Errorf("marshal discovery %s: %w", m.Topic, err)
		}
		if err := p.publish(m.Topic, payload, true); err != nil {
			return err
		}
	}
	return nil
}

// PublishState publishes a job's current state to its state topic. The message
// is retained so the broker holds the last value: backups run at most daily, so
// without retention a Home Assistant restart (or a fresh entity racing the
// state that follows its discovery config) leaves the sensors "unavailable"
// until the next run. Retaining diverges from restic-backup.sh's fire-and-forget
// mosquitto_pub, but keeps the HA sensors populated between runs.
func (p *Publisher) PublishState(jobID string, st State) error {
	topic := fmt.Sprintf("%s/%s/state", p.cfg.TopicPrefix, jobID)
	payload, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	return p.publish(topic, payload, true)
}

func (p *Publisher) publish(topic string, payload []byte, retain bool) error {
	tok := p.client.Publish(topic, 0, retain, payload)
	if !tok.WaitTimeout(10 * time.Second) {
		return fmt.Errorf("mqtt publish %s: timed out", topic)
	}
	if err := tok.Error(); err != nil {
		return fmt.Errorf("mqtt publish %s: %w", topic, err)
	}
	return nil
}
