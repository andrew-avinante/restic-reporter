#!/bin/sh
# restic-reporter installer.
#
# Usage (from a shell):
#   curl -fsSL https://raw.githubusercontent.com/andrew-avinante/restic-reporter/main/install.sh | sh
#   wget -qO- https://raw.githubusercontent.com/andrew-avinante/restic-reporter/main/install.sh | sh
#
# It downloads the prebuilt binary for this OS/arch from GitHub releases,
# installs it to /usr/local/bin, drops the systemd units into place, and then
# (on an interactive terminal) runs a short setup wizard that writes
# /etc/restic-reporter/config.yaml and enables the daily backup timer.
#
# Environment knobs:
#   RESTIC_REPORTER_VERSION   pin a release tag (e.g. v1.2.3); default: latest
#   RESTIC_REPORTER_NO_SETUP  set to 1 to install the binary/units only
#   RESTIC_REPORTER_BINDIR    install dir for the binary (default /usr/local/bin)
#
# The wizard is Linux/systemd-only. On other platforms (or a non-interactive
# pipe) it is skipped and printed instructions tell you how to finish by hand.
set -eu

REPO="andrew-avinante/restic-reporter"
BINDIR="${RESTIC_REPORTER_BINDIR:-/usr/local/bin}"
CONFIG_DIR="/etc/restic-reporter"
CONFIG_PATH="${CONFIG_DIR}/config.yaml"
SYSTEMD_DIR="/etc/systemd/system"
STATE_DIR_DEFAULT="/var/lib/restic-backup"

# ---------------------------------------------------------------------------
# Output helpers
# ---------------------------------------------------------------------------
if [ -t 1 ]; then
	c_bold="$(printf '\033[1m')"; c_dim="$(printf '\033[2m')"
	c_red="$(printf '\033[31m')"; c_grn="$(printf '\033[32m')"
	c_ylw="$(printf '\033[33m')"; c_rst="$(printf '\033[0m')"
else
	c_bold=; c_dim=; c_red=; c_grn=; c_ylw=; c_rst=
fi

info()  { printf '%s==>%s %s\n' "$c_grn" "$c_rst" "$*"; }
step()  { printf '%s ->%s %s\n' "$c_dim" "$c_rst" "$*"; }
warn()  { printf '%swarning:%s %s\n' "$c_ylw" "$c_rst" "$*" >&2; }
fatal() { printf '%serror:%s %s\n' "$c_red" "$c_rst" "$*" >&2; exit 1; }

have() { command -v "$1" >/dev/null 2>&1; }

# Run a command as root: directly if we already are, else via sudo.
run_root() {
	if [ "$(id -u)" -eq 0 ]; then
		"$@"
	elif have sudo; then
		sudo "$@"
	else
		fatal "this step needs root and sudo is not installed; re-run as root: $*"
	fi
}

cleanup() { [ -n "${WORKDIR:-}" ] && rm -rf "$WORKDIR"; }
trap cleanup EXIT INT TERM

# ---------------------------------------------------------------------------
# Platform detection -> goreleaser asset naming
# ---------------------------------------------------------------------------
detect_platform() {
	os="$(uname -s)"
	arch="$(uname -m)"
	case "$os" in
		Linux) os="linux" ;;
		*) fatal "unsupported OS '$os'. Prebuilt binaries are linux-only; build from source with 'go install github.com/${REPO}@latest'." ;;
	esac
	case "$arch" in
		x86_64 | amd64) arch="amd64" ;;
		aarch64 | arm64) arch="arm64" ;;
		*) fatal "unsupported architecture '$arch'. Only amd64/arm64 have prebuilt binaries; build from source with 'go install github.com/${REPO}@latest'." ;;
	esac
	PLATFORM_OS="$os"
	PLATFORM_ARCH="$arch"
}

# Fetch a URL to stdout. Prefers curl, falls back to wget.
fetch() {
	if have curl; then
		curl -fsSL "$1"
	elif have wget; then
		wget -qO- "$1"
	else
		fatal "need curl or wget to download files"
	fi
}

# Fetch a URL to a file.
fetch_to() {
	if have curl; then
		curl -fsSL -o "$2" "$1"
	elif have wget; then
		wget -qO "$2" "$1"
	else
		fatal "need curl or wget to download files"
	fi
}

# Resolve the release tag to install (latest unless pinned).
resolve_version() {
	if [ -n "${RESTIC_REPORTER_VERSION:-}" ]; then
		TAG="$RESTIC_REPORTER_VERSION"
		return
	fi
	step "Resolving latest release..."
	# Parse tag_name out of the GitHub API JSON without needing jq.
	TAG="$(fetch "https://api.github.com/repos/${REPO}/releases/latest" \
		| grep -m1 '"tag_name"' \
		| sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')"
	[ -n "$TAG" ] || fatal "could not determine the latest release. Has a version been tagged yet? Pin one with RESTIC_REPORTER_VERSION=vX.Y.Z."
}

# ---------------------------------------------------------------------------
# Download, verify, install
# ---------------------------------------------------------------------------
install_binary() {
	# goreleaser strips the leading 'v' from the tag for the archive filename,
	# but the release itself is tagged with the 'v'. Keep both.
	ver="${TAG#v}"
	archive="restic-reporter_${ver}_${PLATFORM_OS}_${PLATFORM_ARCH}.tar.gz"
	base="https://github.com/${REPO}/releases/download/${TAG}"

	WORKDIR="$(mktemp -d)"
	info "Installing restic-reporter ${TAG} (${PLATFORM_OS}/${PLATFORM_ARCH})"

	step "Downloading ${archive}"
	fetch_to "${base}/${archive}" "${WORKDIR}/${archive}" \
		|| fatal "download failed: ${base}/${archive}"

	step "Verifying checksum"
	if fetch_to "${base}/checksums.txt" "${WORKDIR}/checksums.txt" 2>/dev/null; then
		( cd "$WORKDIR" && grep " ${archive}\$" checksums.txt > archive.sum ) \
			|| fatal "no checksum entry for ${archive}"
		if have sha256sum; then
			( cd "$WORKDIR" && sha256sum -c archive.sum >/dev/null ) \
				|| fatal "checksum verification failed for ${archive}"
		elif have shasum; then
			( cd "$WORKDIR" && shasum -a 256 -c archive.sum >/dev/null ) \
				|| fatal "checksum verification failed for ${archive}"
		else
			warn "no sha256sum/shasum available; skipping checksum verification"
		fi
	else
		warn "could not download checksums.txt; skipping checksum verification"
	fi

	step "Extracting"
	tar -xzf "${WORKDIR}/${archive}" -C "$WORKDIR"

	step "Installing binary to ${BINDIR}/restic-reporter"
	run_root install -d "$BINDIR"
	run_root install -m 0755 "${WORKDIR}/restic-reporter" "${BINDIR}/restic-reporter"

	step "Installing systemd units to ${SYSTEMD_DIR}"
	run_root install -m 0644 "${WORKDIR}/deploy/restic-reporter.service" "${SYSTEMD_DIR}/restic-reporter.service"
	run_root install -m 0644 "${WORKDIR}/deploy/restic-reporter.timer" "${SYSTEMD_DIR}/restic-reporter.timer"

	step "Seeding example config to ${CONFIG_DIR}/config.example.yaml"
	run_root install -d "$CONFIG_DIR"
	run_root install -m 0644 "${WORKDIR}/config.example.yaml" "${CONFIG_DIR}/config.example.yaml"

	info "Binary installed: $("${BINDIR}/restic-reporter" version 2>/dev/null || echo "$TAG")"
}

# ---------------------------------------------------------------------------
# Interactive setup wizard (reads from /dev/tty so it works under `curl | sh`)
# ---------------------------------------------------------------------------
have_systemd() {
	have systemctl && [ -d /run/systemd/system ]
}

# ask "Prompt" "default" -> echoes the answer
ask() {
	_p="$1"; _d="${2:-}"
	if [ -n "$_d" ]; then
		printf '%s [%s]: ' "$_p" "$_d" > /dev/tty
	else
		printf '%s: ' "$_p" > /dev/tty
	fi
	IFS= read -r _a < /dev/tty || _a=""
	[ -z "$_a" ] && _a="$_d"
	printf '%s' "$_a"
}

# ask_required "Prompt" -> loops until non-empty
ask_required() {
	while :; do
		_v="$(ask "$1" "")"
		[ -n "$_v" ] && { printf '%s' "$_v"; return; }
		warn "this value is required"
	done
}

# ask_secret "Prompt" -> reads without echo
ask_secret() {
	printf '%s: ' "$1" > /dev/tty
	stty -echo < /dev/tty 2>/dev/null || true
	IFS= read -r _a < /dev/tty || _a=""
	stty echo < /dev/tty 2>/dev/null || true
	printf '\n' > /dev/tty
	printf '%s' "$_a"
}

# confirm "Prompt" "Y|N" -> returns 0 for yes
confirm() {
	_def="${2:-Y}"
	case "$_def" in
		[Yy]*) _hint="Y/n" ;;
		*) _hint="y/N" ;;
	esac
	printf '%s [%s]: ' "$1" "$_hint" > /dev/tty
	IFS= read -r _a < /dev/tty || _a=""
	[ -z "$_a" ] && _a="$_def"
	case "$_a" in [Yy]*) return 0 ;; *) return 1 ;; esac
}

run_setup() {
	printf '\n%srestic-reporter setup%s\n' "$c_bold" "$c_rst" > /dev/tty
	printf '%sConfiguring %s and the daily backup timer.%s\n\n' "$c_dim" "$CONFIG_PATH" "$c_rst" > /dev/tty

	if run_root test -f "$CONFIG_PATH"; then
		if ! confirm "A config already exists at ${CONFIG_PATH}. Overwrite it?" "N"; then
			info "Keeping existing config. Skipping the wizard."
			enable_timer
			return
		fi
	fi

	# --- restic ---
	printf '%sRestic%s\n' "$c_bold" "$c_rst" > /dev/tty
	password_file="$(ask "Path to the restic repository password file" "/etc/restic/password")"
	restic_binary="$(ask "restic binary (leave blank for 'restic' on PATH)" "")"

	# --- MQTT ---
	printf '\n%sMQTT / Home Assistant%s\n' "$c_bold" "$c_rst" > /dev/tty
	mqtt_host="$(ask_required "MQTT broker host/IP")"
	mqtt_port="$(ask "MQTT broker port" "1883")"
	mqtt_topic="$(ask_required "MQTT topic prefix (e.g. restic/gaming-server)")"
	mqtt_discovery="$(ask "Home Assistant discovery prefix" "homeassistant")"
	mqtt_username="$(ask "MQTT username (blank for anonymous)" "")"
	mqtt_password=""
	if [ -n "$mqtt_username" ]; then
		mqtt_password="$(ask_secret "MQTT password (stored outside the config, blank for none)")"
	fi

	# --- storage / logs ---
	printf '\n%sStorage%s\n' "$c_bold" "$c_rst" > /dev/tty
	state_dir="$(ask "State directory (per-job last-success markers)" "$STATE_DIR_DEFAULT")"
	log_file="$(ask "Log file (blank = stderr/journal only)" "")"

	# --- jobs ---
	printf '\n%sBackup jobs%s %s(one entry per repo/source to back up)%s\n' \
		"$c_bold" "$c_rst" "$c_dim" "$c_rst" > /dev/tty
	jobs_file="$(mktemp)"
	job_ids=" "
	while :; do
		printf '\n' > /dev/tty
		while :; do
			job_id="$(ask_required "  Job id (machine key, e.g. minecraft)")"
			case "$job_ids" in
				*" $job_id "*) warn "job id '$job_id' already used; pick another" ;;
				*) break ;;
			esac
		done
		job_name="$(ask "  Display name (shown in Home Assistant)" "$job_id")"
		job_source="$(ask_required "  Source path to back up")"
		job_repo="$(ask_required "  Restic repository (e.g. sftp:user@host:/path)")"
		{
			printf '  - id: %s\n' "$job_id"
			[ -n "$job_name" ] && printf '    name: %s\n' "$job_name"
			printf '    repo: %s\n' "$job_repo"
			printf '    source: %s\n' "$job_source"
		} >> "$jobs_file"
		job_ids="${job_ids}${job_id} "
		confirm "Add another job?" "Y" || break
	done

	# --- render config ---
	tmp_config="$(mktemp)"
	{
		printf '# Generated by restic-reporter install.sh on %s\n' "$(date -u '+%Y-%m-%dT%H:%M:%SZ')"
		printf 'restic:\n'
		printf '  password_file: %s\n' "$password_file"
		[ -n "$restic_binary" ] && printf '  binary: %s\n' "$restic_binary"
		printf '\n'
		printf 'mqtt:\n'
		printf '  host: %s\n' "$mqtt_host"
		printf '  port: %s\n' "$mqtt_port"
		[ -n "$mqtt_username" ] && printf '  username: %s\n' "$mqtt_username"
		# mqtt.password is intentionally NOT written here; it is passed via a
		# systemd EnvironmentFile so the secret stays out of config.yaml.
		printf '  discovery_prefix: %s\n' "$mqtt_discovery"
		printf '  topic_prefix: %s\n' "$mqtt_topic"
		printf '\n'
		printf 'state_dir: %s\n' "$state_dir"
		[ -n "$log_file" ] && printf 'log_file: %s\n' "$log_file"
		printf '\n'
		printf 'jobs:\n'
		cat "$jobs_file"
	} > "$tmp_config"
	rm -f "$jobs_file"

	step "Validating generated config"
	if ! "${BINDIR}/restic-reporter" validate --config "$tmp_config" > /dev/tty 2>&1; then
		rm -f "$tmp_config"
		fatal "generated config failed validation (see above). Nothing was written."
	fi

	step "Writing ${CONFIG_PATH}"
	run_root install -d "$CONFIG_DIR"
	run_root install -m 0640 "$tmp_config" "$CONFIG_PATH"
	rm -f "$tmp_config"

	step "Ensuring state directory ${state_dir}"
	run_root install -d "$state_dir"

	# Secret -> systemd EnvironmentFile + drop-in override, never the config.
	if [ -n "$mqtt_password" ]; then
		step "Storing MQTT password in ${CONFIG_DIR}/mqtt.env (systemd EnvironmentFile)"
		tmp_env="$(mktemp)"
		printf 'RESTIC_REPORTER_MQTT_PASSWORD=%s\n' "$mqtt_password" > "$tmp_env"
		run_root install -m 0600 "$tmp_env" "${CONFIG_DIR}/mqtt.env"
		rm -f "$tmp_env"

		tmp_override="$(mktemp)"
		{
			printf '[Service]\n'
			printf 'EnvironmentFile=%s/mqtt.env\n' "$CONFIG_DIR"
		} > "$tmp_override"
		run_root install -d "${SYSTEMD_DIR}/restic-reporter.service.d"
		run_root install -m 0644 "$tmp_override" "${SYSTEMD_DIR}/restic-reporter.service.d/override.conf"
		rm -f "$tmp_override"
	fi

	enable_timer
}

enable_timer() {
	step "Reloading systemd"
	run_root systemctl daemon-reload
	step "Enabling and starting restic-reporter.timer"
	run_root systemctl enable --now restic-reporter.timer

	printf '\n' > /dev/tty
	info "Done. restic-reporter is installed and scheduled."
	printf '%sNext run:%s     systemctl list-timers restic-reporter.timer\n' "$c_dim" "$c_rst" > /dev/tty
	printf '%sRun now:%s      sudo systemctl start restic-reporter.service\n' "$c_dim" "$c_rst" > /dev/tty
	printf '%sFollow logs:%s  journalctl -u restic-reporter.service -f\n' "$c_dim" "$c_rst" > /dev/tty
	printf '%sEdit config:%s  sudo $EDITOR %s\n' "$c_dim" "$c_rst" "$CONFIG_PATH" > /dev/tty
}

print_manual_instructions() {
	cat > /dev/tty <<EOF

${c_bold}Setup wizard skipped.${c_rst} The binary and systemd units are installed. To finish:

  1. Create your config:
       sudo cp ${CONFIG_DIR}/config.example.yaml ${CONFIG_PATH}
       sudo \$EDITOR ${CONFIG_PATH}
  2. Validate it:
       restic-reporter validate --config ${CONFIG_PATH}
  3. Enable the daily timer:
       sudo systemctl daemon-reload
       sudo systemctl enable --now restic-reporter.timer

Re-run this installer on an interactive terminal to use the guided wizard.
EOF
}

# ---------------------------------------------------------------------------
main() {
	detect_platform
	resolve_version
	install_binary

	if [ "${RESTIC_REPORTER_NO_SETUP:-}" = "1" ]; then
		info "RESTIC_REPORTER_NO_SETUP=1 set; skipping setup wizard."
		print_manual_instructions
	elif [ ! -t 1 ] || [ ! -r /dev/tty ]; then
		warn "no interactive terminal detected; skipping setup wizard."
		print_manual_instructions
	elif ! have_systemd; then
		warn "systemd not detected; the wizard only supports systemd hosts."
		print_manual_instructions
	else
		run_setup
	fi
}

main "$@"
