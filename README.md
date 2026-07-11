# restic-reporter

A single static Go binary that runs [restic](https://restic.net/) backup jobs
and publishes per-job metrics to MQTT using Home Assistant MQTT discovery — so
each job shows up as its own device in Home Assistant with no manual entity
config.

It is a drop-in replacement for the original `restic-backup.sh` shell script:
same repos, same MQTT topics, same Home Assistant entities, but with **no
runtime dependencies** on the host (no `jq`, no `mosquitto-clients`, no bash
quirks) and config-driven jobs instead of hardcoded functions.

## Why shell out to restic instead of importing it?

restic keeps essentially all of its functionality in internal `internal/`
packages, which Go forbids external modules from importing. There is no stable
library API. So restic-reporter runs the `restic` binary with `--json` and
parses the summary line — exactly what the shell script did. Go buys us the
static binary, a real MQTT client, and testable metric handling, not a library
integration.

## How it works

For each configured job, restic-reporter:

1. Runs `restic -r <repo> --password-file <file> backup <source> --json`.
2. Parses the `message_type == "summary"` object for metrics
   (`files_new`, `data_added`, `snapshot_id`, …).
3. Records the wall-clock duration and a last-success timestamp under
   `state_dir`.
4. Publishes retained Home Assistant discovery configs and a (non-retained)
   state payload to MQTT.

The process exits `0` only if every job succeeds, so it plays well with a
systemd timer or cron.

### Home Assistant entities (per job)

| Sensor                  | unique_id suffix | Source field            |
| ----------------------- | ---------------- | ----------------------- |
| Status                  | `_status`        | `status`                |
| Backup Duration         | `_duration`      | `duration_seconds`      |
| Data Added              | `_data_added`    | `data_added_bytes` → MB |
| Last Successful Backup  | `_last_success`  | `last_success`          |

The topics, `unique_id`s, device identifiers, and value templates are pinned by
tests (`internal/mqtt/mqtt_test.go`) to stay byte-compatible with the original
shell script — changing them would orphan the existing Home Assistant entities.

## Configuration

Copy `config.example.yaml` and edit it:

```yaml
restic:
  password_file: /etc/restic/password
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
```

MQTT `username`/`password` are optional; leave them unset for an anonymous
broker (matching the original script).

### Environment overrides

Any scalar config value can be overridden by an environment variable prefixed
with `RESTIC_REPORTER_`, with `.` replaced by `_`. This is handy for secrets or
per-host tweaks without editing the file:

```sh
RESTIC_REPORTER_MQTT_HOST=10.0.0.9 \
RESTIC_REPORTER_MQTT_PASSWORD=hunter2 \
  restic-reporter --config /etc/restic-reporter/config.yaml
```

Job definitions are file-only (env overrides cover scalars, not the `jobs`
list).

## Build

The target host (`gaming-server`) is a `linux/amd64` vSphere VM, which is the
`make build` default:

```sh
make build          # -> dist/restic-reporter (linux/amd64, static, stripped)
make test           # unit tests
make GOARCH=arm64 build   # e.g. for a Raspberry Pi host
```

## Deploy

```sh
# On the gaming server:
sudo install -m 0755 dist/restic-reporter /usr/local/bin/restic-reporter
sudo mkdir -p /etc/restic-reporter
sudo install -m 0644 config.example.yaml /etc/restic-reporter/config.yaml   # then edit
sudo install -m 0644 deploy/restic-reporter.service /etc/systemd/system/
sudo install -m 0644 deploy/restic-reporter.timer   /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now restic-reporter.timer

# Run once by hand:
sudo systemctl start restic-reporter.service
journalctl -u restic-reporter.service -f
```

## Usage

The CLI is built with [cobra](https://github.com/spf13/cobra) /
[viper](https://github.com/spf13/viper). Invoked with no subcommand it runs the
backups (the form the systemd unit uses):

```sh
restic-reporter --config /etc/restic-reporter/config.yaml   # run all jobs
restic-reporter validate --config config.yaml               # check config, run nothing
restic-reporter version                                     # print build version
restic-reporter --help
```

`validate` is a fast way to confirm a config parses and passes validation before
enabling the timer.
