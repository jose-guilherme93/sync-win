#!/usr/bin/env bash
set -Eeuo pipefail

# SyncWin Agent Installer — resilient, idempotent, auditable.
# This script installs the SyncWin agent as a system service.
#
# Usage (preferred — server injects SYNCWIN_SERVER and SYNCWIN_TOKEN automatically):
#   curl -fsSL https://sync-win.local/install/TOKEN -o /tmp/sync-win-install.sh
#   sudo bash /tmp/sync-win-install.sh
#
# Manual usage:
#   sudo SYNCWIN_SERVER=https://sync-win.local SYNCWIN_TOKEN=xxxxx bash install.sh
#
# Environment variables:
#   SYNCWIN_SERVER          Server URL (required unless injected by server)
#   SYNCWIN_TOKEN           Enrollment token (required unless injected by server)
#   AUTO_INSTALL_DEPS   Set to 1 to install missing dependencies automatically

# =============================================================================
# Configuration
# =============================================================================

SYNCWIN_USER="${SYNCWIN_USER:-sync-win}"
SYNCWIN_GROUP="${SYNCWIN_GROUP:-sync-win}"
SYNCWIN_BINARY="/usr/local/bin/sync-win-agent"
SYNCWIN_CONFIG_DIR="/etc/sync-win"
SYNCWIN_DATA_DIR="/var/lib/sync-win"
SYNCWIN_LOG_DIR="/var/log/sync-win"
SYNCWIN_SERVICE_NAME="sync-win-agent.service"
SYNCWIN_SERVICE_FILE="/etc/systemd/system/${SYNCWIN_SERVICE_NAME}"

# The unit templates live on the server. /app is a path inside the server
# container, so a client can never read it: download_unit_templates fetches them
# into TMP_DIR and fills these in. That is the bug that meant no device ever got
# the update timer. The agent unit also has an inline fallback below; the updater
# units do not, and without them only automatic updates are lost.
SYNCWIN_SERVICE_TEMPLATE=""
SYNCWIN_UPDATE_SERVICE_NAME="sync-win-agent-update.service"
SYNCWIN_UPDATE_TIMER_NAME="sync-win-agent-update.timer"
SYNCWIN_UPDATE_SERVICE_FILE="/etc/systemd/system/${SYNCWIN_UPDATE_SERVICE_NAME}"
SYNCWIN_UPDATE_TIMER_FILE="/etc/systemd/system/${SYNCWIN_UPDATE_TIMER_NAME}"
SYNCWIN_UPDATE_SERVICE_TEMPLATE=""
SYNCWIN_UPDATE_TIMER_TEMPLATE=""
SYNCWIN_UPDATE_PATH_NAME="sync-win-agent-update.path"
SYNCWIN_UPDATE_PATH_FILE="/etc/systemd/system/${SYNCWIN_UPDATE_PATH_NAME}"
SYNCWIN_UPDATE_PATH_TEMPLATE=""
SYNCWIN_UPDATE_REQUEST_FILE="/var/lib/sync-win/update-request"
SYNCWIN_HELPER_SERVICE_NAME="sync-win-agent-helper.service"
SYNCWIN_HELPER_SERVICE_FILE="/etc/systemd/system/${SYNCWIN_HELPER_SERVICE_NAME}"
SYNCWIN_HELPER_SERVICE_TEMPLATE=""
SYNCWIN_AUTO_UPDATE="${SYNCWIN_AUTO_UPDATE:-1}"

# Temp directory for downloads (cleaned up on exit)
TMP_DIR=""

# Colors — disabled when stdout is not a terminal
if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BLUE='\033[0;34m'
    BOLD='\033[1m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    BOLD=''
    NC=''
fi

# =============================================================================
# Logging helpers
# =============================================================================

info()    { printf "${BLUE}[info]${NC}  %s\n" "$*"; }
success() { printf "${GREEN}[  ok]${NC}  %s\n" "$*"; }
warn()    { printf "${YELLOW}[warn]${NC}  %s\n" "$*"; }
error()   { printf "${RED}[FAIL]${NC}  %s\n" "$*" >&2; }
step()    { printf "\n${BOLD}[%s/%s]${NC} %s\n" "$1" "$TOTAL_STEPS" "$2"; }

# =============================================================================
# Cleanup and rollback
# =============================================================================

cleanup() {
    if [ -n "${TMP_DIR:-}" && -d "${TMP_DIR:-}" ]; then
        rm -rf "$TMP_DIR"
    fi
}
trap cleanup EXIT

ROLLBACK_NEEDED=""
ROLLBACK_BACKUP_BINARY=""
ROLLBACK_BACKUP_SERVICE=""

rollback() {
    if [ -z "$ROLLBACK_NEEDED" ]; then
        return
    fi
    warn "Rolling back changes..."
    case "$ROLLBACK_NEEDED" in
        service_installed)
            rm -f "$SYNCWIN_SERVICE_FILE"
            systemctl daemon-reload 2>/dev/null || true
            ;;
        binary_installed)
            if [ -n "$ROLLBACK_BACKUP_BINARY" && -f "$ROLLBACK_BACKUP_BINARY" ]; then
                mv -f "$ROLLBACK_BACKUP_BINARY" "$SYNCWIN_BINARY"
            else
                rm -f "$SYNCWIN_BINARY"
            fi
            rm -f "$SYNCWIN_SERVICE_FILE"
            systemctl daemon-reload 2>/dev/null || true
            ;;
    esac
    ROLLBACK_NEEDED=""
}
trap rollback EXIT

# =============================================================================
# Dependency checks
# =============================================================================

TOTAL_STEPS=10

check_command() {
    command -v "$1" >/dev/null 2>&1
}

require_command() {
    if ! check_command "$1"; then
        error "Missing required command: $1"
        if [ "${AUTO_INSTALL_DEPS:-0}" = "1" ]; then
            auto_install_dep "$1"
        else
            suggest_install "$1"
            exit 1
        fi
    fi
}

suggest_install() {
    local cmd="$1"
    local pkg="$cmd"
    case "$cmd" in
        sha256sum)  pkg="coreutils" ;;
        systemctl)  pkg="systemd" ;;
    esac
    case "${SYNCWIN_DISTRO_ID:-}" in
        arch|manjaro|endeavouros)
            warn "Install with: sudo pacman -S $pkg" ;;
        ubuntu|debian|linuxmint|pop)
            warn "Install with: sudo apt install $pkg" ;;
        fedora)
            warn "Install with: sudo dnf install $pkg" ;;
        rhel|rocky|almalinux|centos)
            warn "Install with: sudo yum install $pkg" ;;
        opensuse*|sles)
            warn "Install with: sudo zypper install $pkg" ;;
        *)
            warn "Please install '$pkg' manually." ;;
    esac
}

auto_install_dep() {
    local cmd="$1"
    local pkg="$cmd"
    case "$cmd" in
        sha256sum) pkg="coreutils" ;;
    esac
    info "Attempting to install $pkg..."
    case "${SYNCWIN_DISTRO_ID:-}" in
        arch|manjaro|endeavouros)
            pacman -S --noconfirm "$pkg" ;;
        ubuntu|debian|linuxmint|pop)
            apt-get update -qq && apt-get install -y -qq "$pkg" ;;
        fedora)
            dnf install -y -q "$pkg" ;;
        rhel|rocky|almalinux|centos)
            yum install -y -q "$pkg" ;;
        opensuse*|sles)
            zypper install -y "$pkg" ;;
        *)
            error "Cannot auto-install '$pkg' on this distribution"
            exit 1
            ;;
    esac
}

# =============================================================================
# Step functions
# =============================================================================

require_root() {
    if [ "$(id -u)" -ne 0 ]; then
        error "This script must be run as root (use sudo)"
        exit 1
    fi
}

detect_os() {
    if [ ! -f /etc/os-release ]; then
        error "Cannot detect OS: /etc/os-release not found"
        exit 1
    fi
    # shellcheck disable=SC1091
    . /etc/os-release
    SYNCWIN_DISTRO_ID="${ID:-unknown}"
    SYNCWIN_DISTRO_NAME="${NAME:-Unknown}"
    SYNCWIN_DISTRO_VERSION="${VERSION_ID:-}"
    success "$SYNCWIN_DISTRO_NAME${SYNCWIN_DISTRO_VERSION:+ $SYNCWIN_DISTRO_VERSION}"
}

detect_arch() {
    local arch
    arch="$(uname -m)"
    case "$arch" in
        x86_64|amd64)
            SYNCWIN_ARCH="amd64" ;;
        aarch64|arm64)
            SYNCWIN_ARCH="arm64" ;;
        *)
            error "Unsupported architecture: $arch"
            exit 1
            ;;
    esac
    success "$SYNCWIN_ARCH"
}

check_dependencies() {
    local missing=()
    for cmd in bash curl uname systemctl; do
        if ! check_command "$cmd"; then
            missing+=("$cmd")
        fi
    done
    if ! check_command sha256sum; then
        missing+=("sha256sum")
    fi
    if [ ${#missing[@]} -gt 0 ]; then
        error "Missing dependencies: ${missing[*]}"
        if [ "${AUTO_INSTALL_DEPS:-0}" = "1" ]; then
            for cmd in "${missing[@]}"; do
                auto_install_dep "$cmd"
            done
        else
            for cmd in "${missing[@]}"; do
                suggest_install "$cmd"
            done
            exit 1
        fi
    fi
    success "All dependencies satisfied"

    # Check for Lynis (optional, for security audits)
    if check_command lynis; then
        success "Lynis is installed (security audits available)"
    else
        warn "Lynis is NOT installed (security audits unavailable)"
        if [ "${SYNCWIN_DISTRO_ID:-}" = "arch" ] || [ "${SYNCWIN_DISTRO_ID:-}" = "manjaro" ] || [ "${SYNCWIN_DISTRO_ID:-}" = "endeavouros" ]; then
            printf "    Install with: ${BOLD}sudo pacman -S lynis${NC}\n"
        else
            printf "    Install with: ${BOLD}sudo apt install lynis${NC}\n"
        fi
    fi
}

validate_environment() {
    if [ -z "${SYNCWIN_SERVER:-}" ]; then
        error "SYNCWIN_SERVER is not set"
        exit 1
    fi
    if [ -z "${SYNCWIN_TOKEN:-}" ]; then
        error "SYNCWIN_TOKEN is not set"
        exit 1
    fi
    # Strip trailing slash from server URL
    SYNCWIN_SERVER="${SYNCWIN_SERVER%/}"
    success "Server: $SYNCWIN_SERVER"
}

create_user() {
    if id "$SYNCWIN_USER" >/dev/null 2>&1; then
        success "User '$SYNCWIN_USER' already exists"
        return
    fi
    info "Creating system user '$SYNCWIN_USER'..."
    useradd --system --no-create-home --shell /usr/sbin/nologin "$SYNCWIN_USER" 2>/dev/null || \
        useradd -r -s /usr/sbin/nologin "$SYNCWIN_USER" 2>/dev/null
    success "User '$SYNCWIN_USER' created"
}

add_docker_group() {
    if ! getent group docker >/dev/null 2>&1; then
        warn "Docker group not found — skipping docker group setup"
        return
    fi
    if id -nG "$SYNCWIN_USER" 2>/dev/null | tr ' ' '\n' | grep -qx "docker"; then
        success "User '$SYNCWIN_USER' already in docker group"
        return
    fi
    usermod -aG docker "$SYNCWIN_USER"
    success "User '$SYNCWIN_USER' added to docker group (Docker socket access)"
}

configure_user_group() {
    local target_user="${SUDO_USER:-}"
    if [ -z "$target_user" ]; then
        success "No SUDO_USER detected, skipping group setup"
        return
    fi
    if id -nG "$target_user" 2>/dev/null | tr ' ' '\n' | grep -qx "$SYNCWIN_GROUP"; then
        success "User '$target_user' already in group '$SYNCWIN_GROUP'"
        return
    fi
    # Create group if it doesn't exist
    if ! getent group "$SYNCWIN_GROUP" >/dev/null 2>&1; then
        groupadd "$SYNCWIN_GROUP" 2>/dev/null || true
    fi
    usermod -aG "$SYNCWIN_GROUP" "$target_user"
    success "User '$target_user' added to group '$SYNCWIN_GROUP'"
}

create_directories() {
    mkdir -p "$SYNCWIN_CONFIG_DIR" "$SYNCWIN_DATA_DIR" "$SYNCWIN_LOG_DIR"
    chown "$SYNCWIN_USER:$SYNCWIN_GROUP" "$SYNCWIN_DATA_DIR" "$SYNCWIN_LOG_DIR"
    chmod 0755 "$SYNCWIN_CONFIG_DIR" "$SYNCWIN_DATA_DIR" "$SYNCWIN_LOG_DIR"
    success "Directories created"
}

backup_existing() {
    ROLLBACK_BACKUP_BINARY=""
    ROLLBACK_BACKUP_SERVICE=""
    if [ -f "$SYNCWIN_BINARY" ]; then
        ROLLBACK_BACKUP_BINARY=$(mktemp "${SYNCWIN_BINARY}.bak.XXXXXX")
        cp -a "$SYNCWIN_BINARY" "$ROLLBACK_BACKUP_BINARY"
        info "Existing binary backed up"
    fi
    if [ -f "$SYNCWIN_SERVICE_FILE" ]; then
        ROLLBACK_BACKUP_SERVICE=$(mktemp "${SYNCWIN_SERVICE_FILE}.bak.XXXXXX")
        cp -a "$SYNCWIN_SERVICE_FILE" "$ROLLBACK_BACKUP_SERVICE"
        info "Existing service file backed up"
    fi
}

download_binary() {
    TMP_DIR=$(mktemp -d /tmp/sync-win-install.XXXXXX)
    local binary_path="${TMP_DIR}/sync-win-agent"

    info "Downloading SyncWin Agent..."
    local curl_opts=(
        --fail
        --silent
        --show-error
        --location
        --retry 3
        --retry-all-errors
        --connect-timeout 10
        --max-time 120
    )

    if ! curl "${curl_opts[@]}" "${SYNCWIN_SERVER}/api/agent/download" -o "$binary_path"; then
        error "Failed to download agent binary from ${SYNCWIN_SERVER}"
        error "Check that the server is reachable from this machine."
        exit 1
    fi

    if [ ! -s "$binary_path" ]; then
        error "Downloaded binary is empty"
        exit 1
    fi

    success "Binary downloaded ($(wc -c < "$binary_path") bytes)"
}

# download_unit_templates fetches the systemd unit templates from the server.
#
# They cannot be read locally: the installer runs on the device, and their old
# path (/app/...) exists only inside the server container. A missing template is
# not fatal — the agent unit has an inline fallback and the updater units only
# affect automatic updates — so this warns and lets the install continue.
download_unit_templates() {
    local curl_opts=(
        --fail
        --silent
        --show-error
        --location
        --retry 3
        --retry-all-errors
        --connect-timeout 10
        --max-time 30
    )
    local fetched=0
    local mapping=(
        "agent:${SYNCWIN_SERVICE_NAME}"
        "update-service:${SYNCWIN_UPDATE_SERVICE_NAME}"
        "update-timer:${SYNCWIN_UPDATE_TIMER_NAME}"
        "update-path:${SYNCWIN_UPDATE_PATH_NAME}"
        "helper:${SYNCWIN_HELPER_SERVICE_NAME}"
    )

    for entry in "${mapping[@]}"; do
        local key="${entry%%:*}"
        local filename="${entry#*:}"
        local target="${TMP_DIR}/${filename}"
        if curl "${curl_opts[@]}" "${SYNCWIN_SERVER}/api/agent/units?name=${key}" -o "$target" 2>/dev/null \
            && grep -q '^\[Unit\]' "$target"; then
            fetched=$((fetched + 1))
        else
            rm -f "$target"
        fi
    done

    [ -f "${TMP_DIR}/${SYNCWIN_SERVICE_NAME}" ] && SYNCWIN_SERVICE_TEMPLATE="${TMP_DIR}/${SYNCWIN_SERVICE_NAME}"
    [ -f "${TMP_DIR}/${SYNCWIN_UPDATE_SERVICE_NAME}" ] && SYNCWIN_UPDATE_SERVICE_TEMPLATE="${TMP_DIR}/${SYNCWIN_UPDATE_SERVICE_NAME}"
    [ -f "${TMP_DIR}/${SYNCWIN_UPDATE_TIMER_NAME}" ] && SYNCWIN_UPDATE_TIMER_TEMPLATE="${TMP_DIR}/${SYNCWIN_UPDATE_TIMER_NAME}"
    [ -f "${TMP_DIR}/${SYNCWIN_UPDATE_PATH_NAME}" ] && SYNCWIN_UPDATE_PATH_TEMPLATE="${TMP_DIR}/${SYNCWIN_UPDATE_PATH_NAME}"
    [ -f "${TMP_DIR}/${SYNCWIN_HELPER_SERVICE_NAME}" ] && SYNCWIN_HELPER_SERVICE_TEMPLATE="${TMP_DIR}/${SYNCWIN_HELPER_SERVICE_NAME}"

    if [ "$fetched" -eq 5 ]; then
        success "Service templates downloaded"
    else
        warn "Only ${fetched}/5 service templates were downloaded from the server"
        warn "The agent will still be installed; automatic updates may be unavailable"
    fi
}

# supplementary_groups_directive prints the SupplementaryGroups= line for groups
# that actually exist here.
#
# systemd refuses to start a unit that names a group the host does not have, so
# a fixed `docker systemd-journal` line made the service fail to start on any
# host without a docker group — an Arch install with no Docker, for example,
# reported "Service installed but not running yet" and nothing more.
supplementary_groups_directive() {
    local groups=""
    if getent group docker >/dev/null 2>&1; then
        groups="docker"
    fi
    # systemd-journal is the narrow grant for journal reads. Fall back to adm,
    # which also has it, only when systemd-journal is absent.
    if getent group systemd-journal >/dev/null 2>&1; then
        groups="${groups}${groups:+ }systemd-journal"
    elif getent group adm >/dev/null 2>&1; then
        groups="${groups}${groups:+ }adm"
    fi
    [ -n "$groups" ] && printf 'SupplementaryGroups=%s' "$groups"
    return 0
}

verify_checksum() {
    info "Downloading checksums..."
    local checksums_path="${TMP_DIR}/checksums.txt"
    local curl_opts=(
        --fail
        --silent
        --show-error
        --location
        --retry 2
        --connect-timeout 10
        --max-time 30
    )

    if ! curl "${curl_opts[@]}" "${SYNCWIN_SERVER}/api/agent/checksums" -o "$checksums_path"; then
        warn "Could not download checksums — skipping integrity verification"
        return
    fi

    if [ ! -s "$checksums_path" ]; then
        warn "Checksums file is empty — skipping integrity verification"
        return
    fi

    local expected_hash
    expected_hash=$(grep "sync-win-agent" "$checksums_path" 2>/dev/null | head -1 | awk '{print $1}')
    if [ -z "$expected_hash" ]; then
        warn "No checksum found for sync-win-agent — skipping verification"
        return
    fi

    local actual_hash
    actual_hash=$(sha256sum "${TMP_DIR}/sync-win-agent" | awk '{print $1}')
    if [ "$expected_hash" != "$actual_hash" ]; then
        error "Checksum mismatch!"
        error "  Expected: $expected_hash"
        error "  Got:      $actual_hash"
        error "The downloaded binary may be corrupted or tampered with."
        exit 1
    fi
    success "SHA256 checksum verified"
}

install_binary() {
    chmod 755 "${TMP_DIR}/sync-win-agent"
    mv -f "${TMP_DIR}/sync-win-agent" "$SYNCWIN_BINARY"
    ROLLBACK_NEEDED="binary_installed"
    success "Binary installed to $SYNCWIN_BINARY"
}

install_systemd_service() {
    local service_content
    if [ -f "$SYNCWIN_SERVICE_TEMPLATE" ]; then
        service_content=$(<"$SYNCWIN_SERVICE_TEMPLATE")
    else
        service_content='[Unit]
Description=SyncWin Linux Agent
After=network-online.target
Wants=network-online.target
StartLimitIntervalSec=300
StartLimitBurst=5

[Service]
Type=simple
ExecStart=/usr/local/bin/sync-win-agent daemon --server {{SERVER_URL}} --device-id {{DEVICE_ID}} --device-token {{DEVICE_TOKEN}}
Restart=always
RestartSec=5

User=sync-win
Group=sync-win
# systemd-journal (or adm) is what makes the device Logs screen work: journal
# files are mode 0640 root:systemd-journal, so an agent outside that group gets
# a permission error from journalctl and reports no logs at all. docker is for
# the container collectors.
SupplementaryGroups=docker systemd-journal

NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=no
PrivateTmp=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectControlGroups=yes
RestrictSUIDSGID=yes
RestrictRealtime=yes
RestrictNamespaces=yes
# StateDirectory makes systemd create /var/lib/sync-win owned by the service
# user and add it to the writable set. Without it the state file lands under a
# home that does not exist, inside a filesystem ProtectSystem=strict mounted
# read-only, and every restart forgets which files were already uploaded.
StateDirectory=sync-win
# The leading '-' makes systemd skip an entry that does not exist. Without it a
# host with no Docker fails to start the unit at all:
#   Failed to set up mount namespacing: /run/systemd/unit-root/run/docker.sock:
#   No such file or directory
# The same applies to a missing directory: an optional path must be marked
# optional, or the agent refuses to run on a machine that simply lacks it.
ReadWritePaths=/var/log/sync-win /etc/sync-win -/var/run/docker.sock

[Install]
WantedBy=multi-user.target'
    fi

    ROLLBACK_NEEDED="service_installed"
    systemctl stop "$SYNCWIN_SERVICE_NAME" 2>/dev/null || true

    echo "$service_content" > "$SYNCWIN_SERVICE_FILE"

    # Recompute the supplementary groups for this host. The template cannot be
    # trusted for this: it lists docker, and systemd refuses to start a unit that
    # names a group the host does not have. See supplementary_groups_directive.
    local directive
    directive="$(supplementary_groups_directive)"
    if [ -n "$directive" ]; then
        if grep -q '^SupplementaryGroups=' "$SYNCWIN_SERVICE_FILE"; then
            sed -i "s|^SupplementaryGroups=.*|${directive}|" "$SYNCWIN_SERVICE_FILE"
        else
            sed -i "/^\[Service\]/a ${directive}" "$SYNCWIN_SERVICE_FILE"
        fi
        info "Supplementary groups: ${directive#SupplementaryGroups=}"
    else
        sed -i '/^SupplementaryGroups=/d' "$SYNCWIN_SERVICE_FILE"
        warn "No supplementary groups available; the agent will run without docker or journal access"
    fi

    systemctl daemon-reload
    success "Systemd service installed"
}

install_auto_update() {
    if [ "$SYNCWIN_AUTO_UPDATE" != "1" ]; then
        warn "Automatic agent updates disabled (SYNCWIN_AUTO_UPDATE=$SYNCWIN_AUTO_UPDATE)"
        return 0
    fi
    # Missing templates used to be a warning and an early return, which is how a
    # device ended up with no way to ever receive a fix. It is now only reached
    # if the download failed, and it stays a warning rather than aborting:
    # losing automatic updates is bad, but refusing to install the agent at all
    # is worse, and the rollback trap made the old hard failure do exactly that.
    local missing=""
    for tpl in "$SYNCWIN_UPDATE_SERVICE_TEMPLATE" "$SYNCWIN_UPDATE_TIMER_TEMPLATE" "$SYNCWIN_UPDATE_PATH_TEMPLATE"; do
        [ -n "$tpl" ] && [ -f "$tpl" ] || missing="$missing $(basename "${tpl:-unknown}")"
    done
    if [ -n "$missing" ]; then
        warn "Updater templates missing:${missing}"
        warn "Automatic updates were not enabled; re-run this installer later"
        return 0
    fi

    sed \
        -e "s|{{AGENT_BINARY}}|${SYNCWIN_BINARY}|g" \
        -e "s|{{SERVER_URL}}|${SYNCWIN_SERVER}|g" \
        -e "s|{{SERVICE_NAME}}|${SYNCWIN_SERVICE_NAME}|g" \
        -e "s|{{USER_FLAG}}||g" \
        -e "s|{{DEVICE_ID}}|${DEVICE_ID}|g" \
        -e "s|{{DEVICE_TOKEN}}|${DEVICE_TOKEN}|g" \
        "$SYNCWIN_UPDATE_SERVICE_TEMPLATE" > "$SYNCWIN_UPDATE_SERVICE_FILE"
    cp "$SYNCWIN_UPDATE_TIMER_TEMPLATE" "$SYNCWIN_UPDATE_TIMER_FILE"
    cp "$SYNCWIN_UPDATE_PATH_TEMPLATE" "$SYNCWIN_UPDATE_PATH_FILE"

    # The daemon writes this file to request an update. It must exist and be
    # writable by the agent user before the path unit is armed.
    install -d -o "$SYNCWIN_USER" -g "$SYNCWIN_USER" -m 0755 "$(dirname "$SYNCWIN_UPDATE_REQUEST_FILE")"
    install -o "$SYNCWIN_USER" -g "$SYNCWIN_USER" -m 0644 /dev/null "$SYNCWIN_UPDATE_REQUEST_FILE"

    systemctl daemon-reload
    local ok=1
    systemctl enable --now "$SYNCWIN_UPDATE_TIMER_NAME" >/dev/null 2>&1 \
        || { warn "Could not enable ${SYNCWIN_UPDATE_TIMER_NAME}"; ok=0; }
    # The path unit is what lets a device whose timer was never installed still
    # upgrade itself, so a failure here matters more than the timer's.
    systemctl enable --now "$SYNCWIN_UPDATE_PATH_NAME" >/dev/null 2>&1 \
        || { warn "Could not enable ${SYNCWIN_UPDATE_PATH_NAME}"; ok=0; }

    if [ "$ok" = "1" ]; then
        success "Automatic agent updates enabled (timer every 15 minutes, plus on demand)"
    else
        warn "Automatic updates are only partially enabled; check the units above"
    fi
}

# install_remote_access_helper installs the privileged helper used by
# interactive remote access, records the account that ran the installer as the
# default SSH target, and checks for an ssh client.
#
# None of this is fatal: remote access is opt-in and off by default, so a host
# where it cannot be set up loses a feature, not its agent.
install_remote_access_helper() {
    if [ -n "$SYNCWIN_HELPER_SERVICE_TEMPLATE" ] && [ -f "$SYNCWIN_HELPER_SERVICE_TEMPLATE" ]; then
        cp "$SYNCWIN_HELPER_SERVICE_TEMPLATE" "$SYNCWIN_HELPER_SERVICE_FILE"
        systemctl daemon-reload
        if systemctl enable --now "$SYNCWIN_HELPER_SERVICE_NAME" >/dev/null 2>&1; then
            success "Remote access helper installed"
        else
            warn "Could not start ${SYNCWIN_HELPER_SERVICE_NAME}; remote access will be unavailable"
        fi
    else
        warn "Remote access helper unit was not downloaded; remote access will be unavailable"
    fi

    # The terminal runs ssh and ssh-keygen; both come from the OpenSSH client.
    if ! command -v ssh >/dev/null 2>&1 || ! command -v ssh-keygen >/dev/null 2>&1; then
        warn "OpenSSH client not found; install openssh-client to use remote access"
    fi

    # Record the account that ran the installer as the default SSH target. This
    # goes through the agent's own `set`, so the policy path and the fail-closed
    # defaults live in one place. It never enables remote access itself.
    local target_user="${SUDO_USER:-}"
    if [ -n "$target_user" ] && [ "$target_user" != "root" ] && id "$target_user" >/dev/null 2>&1; then
        if "$SYNCWIN_BINARY" set ssh-user "$target_user" >/dev/null 2>&1; then
            success "Default remote-access user set to '$target_user'"
        else
            warn "Could not record the default remote-access user"
        fi
    fi

    # Remote access is off unless the operator enabled it on the Add-device
    # screen, which is the install-time consent baked into this command.
    if [ "${SYNCWIN_SSH_ON_INSTALL:-0}" = "1" ]; then
        if "$SYNCWIN_BINARY" set ssh on >/dev/null 2>&1; then
            success "Remote access enabled (SSH)"
        else
            warn "Could not enable remote access; run: sudo $SYNCWIN_BINARY set ssh on"
        fi
    else
        info "Remote access stays off until you run: sudo $SYNCWIN_BINARY set ssh on"
    fi
}

# read_machine_id returns the host's stable machine identifier.
#
# This is what lets a reinstall adopt its own device record instead of creating a
# second one with the same hostname. /etc/machine-id is written once per OS
# installation and survives agent reinstalls, unlike anything under /home.
read_machine_id() {
    local id=""
    if [ -r /etc/machine-id ]; then
        id="$(tr -d '[:space:]' < /etc/machine-id)"
    fi
    if [ -z "$id" ] && [ -r /var/lib/dbus/machine-id ]; then
        id="$(tr -d '[:space:]' < /var/lib/dbus/machine-id)"
    fi
    printf '%s' "$id"
}

enroll_device() {
    info "Registering device with server..."
    local enroll_payload
    local host_name kernel_ver agent_ver machine_id
    host_name="$(hostname | tr -d '\n')"
    kernel_ver="$(uname -r | tr -d '\n')"
    agent_ver="$(get_installed_version | tr -d '\n')"
    machine_id="$(read_machine_id)"
    enroll_payload=$(cat <<EOF
{
    "token": "${SYNCWIN_TOKEN}",
    "hostname": "${host_name}",
    "architecture": "${SYNCWIN_ARCH}",
    "os": "${SYNCWIN_DISTRO_ID}",
    "kernel": "${kernel_ver}",
    "agent_version": "${agent_ver}",
    "machine_id": "${machine_id}"
}
EOF
)
    local response
    local http_code
    local tmp_response="${TMP_DIR}/enroll-response.json"

    http_code=$(curl \
        --silent \
        --show-error \
        --location \
        --retry 2 \
        --connect-timeout 10 \
        --max-time 30 \
        -X POST \
        -H "Content-Type: application/json" \
        -d "$enroll_payload" \
        -o "$tmp_response" \
        -w "%{http_code}" \
        "${SYNCWIN_SERVER}/api/agent/enroll" 2>/dev/null) || true

    if [ "$http_code" -lt 200 ] || [ "$http_code" -ge 300 ]; then
        error "Enrollment failed (HTTP $http_code)"
        if [ -f "$tmp_response" ]; then
            local error_msg
            error_msg=$(cat "$tmp_response" 2>/dev/null || echo "unknown error")
            error "Server response: $error_msg"
        fi
        error ""
        error "Possible causes:"
        error "  - Enrollment token has expired (tokens are valid for 15 minutes)"
        error "  - Token has already been used"
        error "  - Server is unreachable"
        error ""
        error "To retry, generate a new install command from the dashboard."
        exit 1
    fi

    if [ ! -f "$tmp_response" ]; then
        error "No response from server during enrollment"
        exit 1
    fi

    DEVICE_ID=$(grep -o '"device_id":"[^"]*"' "$tmp_response" | head -1 | cut -d'"' -f4)
    DEVICE_TOKEN=$(grep -o '"device_token":"[^"]*"' "$tmp_response" | head -1 | cut -d'"' -f4)

    if [ -z "$DEVICE_ID" ] || [ -z "$DEVICE_TOKEN" ]; then
        error "Invalid enrollment response"
        error "Response: $(cat "$tmp_response")"
        exit 1
    fi

    success "Device registered (ID: $DEVICE_ID)"
}

get_installed_version() {
    if [ -x "$SYNCWIN_BINARY" ]; then
        "$SYNCWIN_BINARY" --version 2>/dev/null | head -1 || echo "unknown"
    else
        echo "none"
    fi
}

configure_service() {
    if [ -z "${DEVICE_ID:-}" ] || [ -z "${DEVICE_TOKEN:-}" ]; then
        error "Cannot configure service: device ID or token missing"
        exit 1
    fi

    local current_user="${SUDO_USER:-root}"
    local home_dir
    home_dir=$(getent passwd "$current_user" | cut -d: -f6)
    if [ -z "$home_dir" ]; then
        home_dir="/home/$current_user"
    fi

    sed -i \
        -e "s|{{SERVER_URL}}|${SYNCWIN_SERVER}|g" \
        -e "s|{{DEVICE_ID}}|${DEVICE_ID}|g" \
        -e "s|{{DEVICE_TOKEN}}|${DEVICE_TOKEN}|g" \
        "$SYNCWIN_SERVICE_FILE"

    systemctl daemon-reload
    success "Service configured"
}

start_service() {
    systemctl enable "$SYNCWIN_SERVICE_NAME" >/dev/null 2>&1
    systemctl restart "$SYNCWIN_SERVICE_NAME"
    sleep 2
    if systemctl is-active --quiet "$SYNCWIN_SERVICE_NAME"; then
        success "Service is running"
    else
        warn "Service installed but not running yet"
        warn "Check: systemctl status $SYNCWIN_SERVICE_NAME"
    fi
}

# verify_agent_readiness checks the two things that silently break the dashboard
# without breaking the service: the unit missing systemd-journal (no device logs
# anywhere) and the update timer not running (no agent ever receives a fix).
# Both used to be a warning nobody saw, which is how a broken Logs screen looked
# identical to a healthy one.
verify_agent_readiness() {
    local problems=0

    # Reinstall rather than patch. A unit that is missing entirely (systemd then
    # reports Loaded: not-found while the process keeps running from a
    # definition held in memory) or one that predates systemd-journal is fixed
    # the same way: regenerate it from the current template.
    if [ ! -f "$SYNCWIN_SERVICE_FILE" ] || ! grep -qE "systemd-journal|adm" "$SYNCWIN_SERVICE_FILE"; then
        if [ -f "$SYNCWIN_SERVICE_FILE" ]; then
            warn "The agent unit has no journal group; the Logs screen will stay empty."
        else
            warn "The agent unit file is missing; reinstalling it."
        fi
        install_systemd_service >/dev/null 2>&1
        configure_service >/dev/null 2>&1
        systemctl daemon-reload
        systemctl restart "$SYNCWIN_SERVICE_NAME" >/dev/null 2>&1
        if [ -f "$SYNCWIN_SERVICE_FILE" ] && grep -qE "systemd-journal|adm" "$SYNCWIN_SERVICE_FILE"; then
            success "Agent unit regenerated with journal access"
        else
            problems=$((problems + 1))
            warn "Could not regenerate the agent unit."
        fi
    fi

    if ! systemctl is-enabled --quiet "$SYNCWIN_UPDATE_TIMER_NAME" 2>/dev/null; then
        warn "The auto-update timer is not enabled; this agent will never receive fixes."
        if systemctl enable --now "$SYNCWIN_UPDATE_TIMER_NAME" >/dev/null 2>&1; then
            success "Enabled ${SYNCWIN_UPDATE_TIMER_NAME} (checks every 15 minutes)"
        else
            problems=$((problems + 1))
            warn "Could not enable ${SYNCWIN_UPDATE_TIMER_NAME}."
            warn "Enable it manually: systemctl enable --now ${SYNCWIN_UPDATE_TIMER_NAME}"
        fi
    fi

    if ! systemctl is-enabled --quiet "$SYNCWIN_UPDATE_PATH_NAME" 2>/dev/null; then
        if [ -f "$SYNCWIN_UPDATE_PATH_FILE" ] && systemctl enable --now "$SYNCWIN_UPDATE_PATH_NAME" >/dev/null 2>&1; then
            success "Enabled ${SYNCWIN_UPDATE_PATH_NAME} (updates on agent request)"
        else
            problems=$((problems + 1))
            warn "Could not enable ${SYNCWIN_UPDATE_PATH_NAME}; on-demand updates are unavailable."
        fi
    fi

    if [ "$problems" -gt 0 ]; then
        warn "$problems agent readiness problem(s) could not be fixed automatically."
        warn "The dashboard's Logs screen will not work until these are resolved."
    fi
}

print_summary() {
    local current_user="${SUDO_USER:-}"
    printf "\n"
    printf "${BOLD}========================================${NC}\n"
    printf "${BOLD}  SyncWin Agent Installation Complete${NC}\n"
    printf "${BOLD}========================================${NC}\n"
    printf "\n"
    printf "  Server:      %s\n" "$SYNCWIN_SERVER"
    printf "  Device ID:   %s\n" "${DEVICE_ID:-unknown}"
    printf "  Binary:      %s\n" "$SYNCWIN_BINARY"
    printf "  Service:     %s\n" "$SYNCWIN_SERVICE_FILE"
    printf "  Config:      %s\n" "$SYNCWIN_CONFIG_DIR"
    printf "  Data:        %s\n" "$SYNCWIN_DATA_DIR"
    printf "  Logs:        %s\n" "$SYNCWIN_LOG_DIR"
    printf "\n"
    printf "  ${BOLD}Useful commands:${NC}\n"
    printf "    systemctl status $SYNCWIN_SERVICE_NAME\n"
    printf "    journalctl -u $SYNCWIN_SERVICE_NAME -f\n"
    printf "    systemctl restart $SYNCWIN_SERVICE_NAME\n"
    printf "\n"
    if [ -n "$current_user" ] && [ "$current_user" != "root" ]; then
        printf "  ${BOLD}Note:${NC} User '$current_user' was added to the '$SYNCWIN_GROUP' group.\n"
        printf "  They may need to log out and back in for group changes to take effect.\n"
        printf "\n"
    fi
}

# =============================================================================
# Main
# =============================================================================

main() {
    require_root
    step "1" "Detecting operating system..."
    detect_os
    step "2" "Detecting architecture..."
    detect_arch
    step "3" "Validating environment..."
    validate_environment
    step "4" "Checking dependencies..."
    check_dependencies
    step "5" "Creating user and directories..."
    create_user
    add_docker_group
    configure_user_group
    create_directories
    step "6" "Backing up existing installation..."
    backup_existing
    step "7" "Downloading SyncWin Agent..."
    download_binary
    download_unit_templates
    step "8" "Verifying integrity..."
    verify_checksum
    step "9" "Installing binary and service..."
    install_binary
    install_systemd_service
    step "10" "Enrolling device and starting service..."
    enroll_device
    configure_service
    # Set up the remote-access helper and its policy before the agent starts, so
    # the agent's very first inventory already carries the operator's choice.
    install_remote_access_helper
    start_service
    install_auto_update
    verify_agent_readiness

    # Reaching this point means the install completed, so the rollback trap must
    # be disarmed. Nothing did that before, so every successful install rolled
    # back on exit and deleted the unit it had just written. The service kept
    # running from a definition systemd no longer had -- "Loaded: not-found"
    # while "Active: active" -- and vanished at the next restart or boot.
    ROLLBACK_NEEDED=""

    print_summary
}

main "$@"
