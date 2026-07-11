# restic-reporter

`restic-reporter` runs a set of configured [restic](https://restic.net/) backup
jobs and publishes per-job metrics to an MQTT broker using [Home Assistant MQTT
discovery](https://www.home-assistant.io/integrations/mqtt/#mqtt-discovery). Point it
at a config file describing your repos and sources, run it on a schedule, and each job
shows up in Home Assistant as a device with status/duration/data-added/last-success
sensors — no `jq`, `mosquitto_pub`, or shell scripting required on the host.

It is a single static Go binary. MQTT reporting is best-effort: a broker that's down
or unreachable never blocks or fails a backup, it just means that run isn't reported.

## Requirements

- Go 1.24+ (only needed to build/install)
- [`restic`](https://restic.net/) on `PATH` (or point `restic.binary` at it)
- An MQTT broker (for Home Assistant integration); optional if you don't care about
  MQTT reporting and just want the scheduled backups

## Install

```sh
go install github.com/andrew-avinante/restic-reporter@latest
```

This puts `restic-reporter` in `$(go env GOPATH)/bin` (usually `~/go/bin` — make sure
it's on your `PATH`).

For a deployment target (e.g. the systemd unit below expects
`/usr/local/bin/restic-reporter`), build and copy it explicitly instead:

```sh
git clone git@github.com:andrew-avinante/restic-reporter.git
cd restic-reporter
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/andrew-avinante/restic-reporter/cmd.version=$(git describe --tags --always --dirty)" -o restic-reporter .
sudo install -m 0755 restic-reporter /usr/local/bin/restic-reporter
```

Cross-compiling for a different host is the usual `GOOS`/`GOARCH` dance, e.g. for a
linux/amd64 server from another platform:

```sh
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o restic-reporter .
```

## Usage

```
restic-reporter [--config PATH]      # run every configured job
restic-reporter validate [--config PATH]   # load + validate config, run nothing
restic-reporter version              # print the build version
```

- `--config` is a persistent flag on every subcommand. It defaults to
  `/etc/restic-reporter/config.yaml`.
- Running with no subcommand runs every job in `jobs:` in order and exits non-zero if
  any backup failed. MQTT publish failures are logged as warnings but never affect the
  exit code.
- `Ctrl-C` / `SIGTERM` cancels an in-flight restic process cleanly.

## Configuration

Copy [`config.example.yaml`](config.example.yaml) to `config.yaml` (or wherever
`--config` points, e.g. `/etc/restic-reporter/config.yaml`) and edit it:

```yaml
restic:
  password_file: /etc/restic/password   # passed to restic --password-file
  binary: restic                        # optional; defaults to "restic" on PATH

mqtt:
  host: 192.168.18.131
  port: 1883                            # optional; defaults to 1883
  # username / password are optional; leave unset for an anonymous broker
  # username: ""
  # password: ""
  discovery_prefix: homeassistant       # optional; defaults to "homeassistant"
  topic_prefix: restic/gaming-server

state_dir: /var/lib/restic-backup       # per-job last-success timestamps live here
log_file: /tmp/restic-backup.log        # restic output + diagnostics; empty = stderr only

jobs:
  - id: minecraft
    name: Minecraft
    repo: sftp:restic-backup@192.168.0.1:/mnt/backups/gaming-server/minecraft
    source: /opt/game-servers/minecraft/data

  - id: vintage_story
    name: Vintage Story
    repo: sftp:restic-backup@192.168.0.1:/mnt/backups/gaming-server/vintage-story
    source: /opt/game-servers/vintagestory/data
```

### Config reference

| Key | Required | Default | Description |
|---|---|---|---|
| `restic.password_file` | yes | — | Path passed to `restic --password-file`. The password itself is never read by restic-reporter. |
| `restic.binary` | no | `restic` | Restic executable to run. |
| `mqtt.host` | yes | — | Broker hostname/IP. |
| `mqtt.port` | no | `1883` | Broker port. |
| `mqtt.username` | no | — | Broker username. Leave unset for an anonymous broker. |
| `mqtt.password` | no | — | Broker password. |
| `mqtt.discovery_prefix` | no | `homeassistant` | HA discovery topic prefix. |
| `mqtt.topic_prefix` | yes | — | Prefix for state topics, e.g. `restic/gaming-server`. |
| `state_dir` | yes | — | Directory for per-job `<id>_last_success` marker files. Created on startup if missing. |
| `log_file` | no | stderr only | File that restic's JSON output and diagnostics are appended to (also mirrored to stderr). |
| `jobs[].id` | yes | — | Machine-readable job key. Used in MQTT topics and HA `unique_id`s — **changing it orphans existing HA entities.** Must be unique. |
| `jobs[].name` | no | — | Human-readable label shown in Home Assistant. |
| `jobs[].repo` | yes | — | Restic repository, e.g. an `sftp:` or local path target. |
| `jobs[].source` | yes | — | Path to back up. |

Validate a config without running anything:

```sh
restic-reporter validate --config /etc/restic-reporter/config.yaml
```

### Environment variable overrides

Every scalar (non-`jobs`) key can be overridden at runtime with a
`RESTIC_REPORTER_`-prefixed environment variable, dots replaced with underscores. This
is useful for keeping secrets (like `mqtt.password`) out of the config file:

```sh
RESTIC_REPORTER_MQTT_PASSWORD=hunter2 restic-reporter --config config.yaml
```

| Env var | Overrides |
|---|---|
| `RESTIC_REPORTER_RESTIC_PASSWORD_FILE` | `restic.password_file` |
| `RESTIC_REPORTER_RESTIC_BINARY` | `restic.binary` |
| `RESTIC_REPORTER_MQTT_HOST` | `mqtt.host` |
| `RESTIC_REPORTER_MQTT_PORT` | `mqtt.port` |
| `RESTIC_REPORTER_MQTT_USERNAME` | `mqtt.username` |
| `RESTIC_REPORTER_MQTT_PASSWORD` | `mqtt.password` |
| `RESTIC_REPORTER_MQTT_DISCOVERY_PREFIX` | `mqtt.discovery_prefix` |
| `RESTIC_REPORTER_MQTT_TOPIC_PREFIX` | `mqtt.topic_prefix` |
| `RESTIC_REPORTER_STATE_DIR` | `state_dir` |
| `RESTIC_REPORTER_LOG_FILE` | `log_file` |

`jobs` is file-only; there's no env override for the job list.

## Scheduling

### systemd (recommended)

Unit files are provided in [`deploy/`](deploy). Install and enable them:

```sh
sudo install -m 0755 restic-reporter /usr/local/bin/restic-reporter
sudo mkdir -p /etc/restic-reporter
sudo cp config.example.yaml /etc/restic-reporter/config.yaml   # then edit it
sudo install -m 0644 deploy/restic-reporter.service /etc/systemd/system/
sudo install -m 0644 deploy/restic-reporter.timer /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now restic-reporter.timer
```

[`restic-reporter.service`](deploy/restic-reporter.service) is a `oneshot` unit that
runs `restic-reporter --config /etc/restic-reporter/config.yaml`, niced and I/O-throttled
so backups don't compete with foreground work. [`restic-reporter.timer`](deploy/restic-reporter.timer)
fires it daily at 03:30 local time and is `Persistent=true`, so a run that was missed
(e.g. the machine was off) fires as soon as it's back up.

Check it:

```sh
systemctl list-timers restic-reporter.timer   # see next scheduled run
sudo systemctl start restic-reporter.service  # trigger a run immediately
journalctl -u restic-reporter.service -f      # follow logs
```

Adjust the schedule by editing `OnCalendar=` in the timer unit (systemd
`OnCalendar` syntax, e.g. `*-*-* 03:30:00` for daily at 03:30) and re-running
`daemon-reload`.

### crontab

If you'd rather use cron than systemd timers, skip the `.timer`/`.service` units and
add a crontab entry instead (`crontab -e`, or a drop-in under `/etc/cron.d/`):

```cron
# m h  dom mon dow  command
30 3 * * * /usr/local/bin/restic-reporter --config /etc/restic-reporter/config.yaml
```

## Home Assistant / MQTT sensors

For each configured job, restic-reporter publishes a retained MQTT discovery config
per sensor (topic `<discovery_prefix>/sensor/restic_<job.id>/<sensor>/config`) and a
single non-retained state payload per run (topic `<topic_prefix>/<job.id>/state`).
Discovery configs are republished on every run, so Home Assistant picks up a new job
the first time it backs up successfully.

Each job appears in Home Assistant as its own device (`Restic Backup - <job.name>`,
manufacturer `restic`, model `SFTP backup`) with four sensors:

| Sensor | Value | Unit / device class |
|---|---|---|
| Status | `value_json.status` (`success` or `failed`) | — (icon: `mdi:backup-restore`) |
| Backup Duration | `value_json.duration_seconds` | seconds, `device_class: duration` |
| Data Added | `value_json.data_added_bytes` converted to MB, rounded to 2dp | MB (icon: `mdi:database-arrow-up`) |
| Last Successful Backup | `value_json.last_success` (ISO 8601 UTC) | `device_class: timestamp` |

The state payload behind those sensors:

```json
{
  "status": "success",
  "timestamp": "2026-07-11T03:30:04Z",
  "duration_seconds": 42,
  "files_new": 3,
  "files_changed": 12,
  "files_unmodified": 481,
  "data_added_bytes": 1048576,
  "total_files_processed": 496,
  "total_bytes_processed": 20971520,
  "snapshot_id": "a1b2c3d4",
  "last_success": "2026-07-11T03:30:04Z",
  "error": ""
}
```

`last_success` is read back from `<state_dir>/<job.id>_last_success`, so it persists
across runs even if a later backup fails; `error` holds the tail of restic's stderr
when `status` is `failed`.

These topics and payload shapes intentionally match a prior shell-script
implementation byte-for-byte, so existing Home Assistant entities/dashboards keep
working unchanged if you're migrating from it.

## Development

```sh
go build -o restic-reporter .   # build
go test ./...                   # run tests
go vet ./...                    # vet
```

Project layout:

- `main.go` — entrypoint; wires up signal handling and hands off to `cmd`.
- `cmd/` — cobra CLI: root command (runs backups), `validate`, `version`.
- `internal/config/` — YAML config loading/validation via viper.
- `internal/restic/` — shells out to `restic backup --json` and parses the summary.
- `internal/mqtt/` — MQTT publishing and Home Assistant discovery payloads.
- `deploy/` — systemd service + timer units.
