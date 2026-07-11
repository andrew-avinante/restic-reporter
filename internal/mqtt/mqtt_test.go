package mqtt

import (
	"encoding/json"
	"reflect"
	"testing"
)

// cfg matches the values from gaming-server/scripts/restic-backup.sh.
var testCfg = Config{
	DiscoveryPrefix: "homeassistant",
	TopicPrefix:     "restic/gaming-server",
}

// TestBuildDiscoveryMatchesShell pins every field that Home Assistant keys on
// (topic, unique_id, device identifiers, value_template) to the exact values
// produced by the original restic-backup.sh. Changing any of these would
// orphan or duplicate the live HA entities.
func TestBuildDiscoveryMatchesShell(t *testing.T) {
	msgs := buildDiscovery(testCfg, "minecraft", "Minecraft")
	if len(msgs) != 4 {
		t.Fatalf("expected 4 discovery sensors, got %d", len(msgs))
	}

	byTopic := map[string]discoveryConfig{}
	for _, m := range msgs {
		byTopic[m.Topic] = m.Config
	}

	const stateTopic = "restic/gaming-server/minecraft/state"
	wantDevice := deviceInfo{
		Identifiers:  []string{"restic_minecraft"},
		Name:         "Restic Backup - Minecraft",
		Manufacturer: "restic",
		Model:        "SFTP backup",
	}

	cases := []struct {
		topic         string
		uniqueID      string
		valueTemplate string
		unit          string
		deviceClass   string
		stateClass    string
		icon          string
		jsonAttrTopic string
	}{
		{
			topic:         "homeassistant/sensor/restic_minecraft/status/config",
			uniqueID:      "restic_minecraft_status",
			valueTemplate: "{{ value_json.status }}",
			icon:          "mdi:backup-restore",
			jsonAttrTopic: stateTopic,
		},
		{
			topic:         "homeassistant/sensor/restic_minecraft/duration/config",
			uniqueID:      "restic_minecraft_duration",
			valueTemplate: "{{ value_json.duration_seconds }}",
			unit:          "s",
			deviceClass:   "duration",
			stateClass:    "measurement",
		},
		{
			topic:         "homeassistant/sensor/restic_minecraft/data_added/config",
			uniqueID:      "restic_minecraft_data_added",
			valueTemplate: "{{ (value_json.data_added_bytes | float / 1048576) | round(2) }}",
			unit:          "MB",
			stateClass:    "measurement",
			icon:          "mdi:database-arrow-up",
		},
		{
			topic:         "homeassistant/sensor/restic_minecraft/last_success/config",
			uniqueID:      "restic_minecraft_last_success",
			valueTemplate: "{{ value_json.last_success }}",
			deviceClass:   "timestamp",
		},
	}

	for _, tc := range cases {
		c, ok := byTopic[tc.topic]
		if !ok {
			t.Errorf("missing discovery topic %q", tc.topic)
			continue
		}
		if c.UniqueID != tc.uniqueID {
			t.Errorf("%s: unique_id = %q, want %q", tc.topic, c.UniqueID, tc.uniqueID)
		}
		if c.StateTopic != stateTopic {
			t.Errorf("%s: state_topic = %q, want %q", tc.topic, c.StateTopic, stateTopic)
		}
		if c.ValueTemplate != tc.valueTemplate {
			t.Errorf("%s: value_template = %q, want %q", tc.topic, c.ValueTemplate, tc.valueTemplate)
		}
		if c.UnitOfMeasurement != tc.unit {
			t.Errorf("%s: unit = %q, want %q", tc.topic, c.UnitOfMeasurement, tc.unit)
		}
		if c.DeviceClass != tc.deviceClass {
			t.Errorf("%s: device_class = %q, want %q", tc.topic, c.DeviceClass, tc.deviceClass)
		}
		if c.StateClass != tc.stateClass {
			t.Errorf("%s: state_class = %q, want %q", tc.topic, c.StateClass, tc.stateClass)
		}
		if c.Icon != tc.icon {
			t.Errorf("%s: icon = %q, want %q", tc.topic, c.Icon, tc.icon)
		}
		if c.JSONAttributesTopic != tc.jsonAttrTopic {
			t.Errorf("%s: json_attributes_topic = %q, want %q", tc.topic, c.JSONAttributesTopic, tc.jsonAttrTopic)
		}
		if !reflect.DeepEqual(c.Device, wantDevice) {
			t.Errorf("%s: device = %+v, want %+v", tc.topic, c.Device, wantDevice)
		}
	}
}

// TestStateJSONKeys pins the state payload keys the HA value_templates read.
func TestStateJSONKeys(t *testing.T) {
	b, err := json.Marshal(State{Status: "success"})
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{
		"status", "timestamp", "duration_seconds", "files_new", "files_changed",
		"files_unmodified", "data_added_bytes", "total_files_processed",
		"total_bytes_processed", "snapshot_id", "last_success", "error",
	} {
		if _, ok := m[key]; !ok {
			t.Errorf("state payload missing key %q", key)
		}
	}
}
