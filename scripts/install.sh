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
SYNCWIN_SERVICE_TEMPLATE="/app/sync-win-agent.service"
SYNCWIN_UPDATE_SERVICE_NAME="sync-win-agent-update.service"
SYNCWIN_UPDATE_TIMER_NAME="sync-win-agent-update.timer"
SYNCWIN_UPDATE_SERVICE_FILE="/etc/systemd/system/${SYNCWIN_UPDATE_SERVICE_NAME}"
SYNCWIN_UPDATE_TIMER_FILE="/etc/systemd/system/${SYNCWIN_UPDATE_TIMER_NAME}"
SYNCWIN_UPDATE_SERVICE_TEMPLATE="/app/sync-win-agent-update.service"
SYNCWIN_UPDATE_TIMER_TEMPLATE="/app/sync-win-agent-update.timer"
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
ReadWritePaths=/var/lib/sync-win /var/log/sync-win /etc/sync-win /var/run/docker.sock

[Install]
WantedBy=multi-user.target'
    fi

    ROLLBACK_NEEDED="service_installed"
    systemctl stop "$SYNCWIN_SERVICE_NAME" 2>/dev/null || true

    echo "$service_content" > "$SYNCWIN_SERVICE_FILE"
    systemctl daemon-reload
    success "Systemd service installed"
}

install_auto_update() {
    if [ "$SYNCWIN_AUTO_UPDATE" != "1" ]; then
        warn "Automatic agent updates disabled (SYNCWIN_AUTO_UPDATE=$SYNCWIN_AUTO_UPDATE)"
        return 0
    fi
    if [ ! -f "$SYNCWIN_UPDATE_SERVICE_TEMPLATE" ] || [ ! -f "$SYNCWIN_UPDATE_TIMER_TEMPLATE" ]; then
        warn "Updater templates not found — automatic updates were not enabled"
        return 0
    fi
    sed \
        -e "s|{{AGENT_BINARY}}|${SYNCWIN_BINARY}|g" \
        -e "s|{{SERVER_URL}}|${SYNCWIN_SERVER}|g" \
        -e "s|{{SERVICE_NAME}}|${SYNCWIN_SERVICE_NAME}|g" \
        -e "s|{{USER_FLAG}}||g" \
        "$SYNCWIN_UPDATE_SERVICE_TEMPLATE" > "$SYNCWIN_UPDATE_SERVICE_FILE"
    cp "$SYNCWIN_UPDATE_TIMER_TEMPLATE" "$SYNCWIN_UPDATE_TIMER_FILE"
    systemctl daemon-reload
    if systemctl enable --now "$SYNCWIN_UPDATE_TIMER_NAME" >/dev/null 2>&1; then
        success "Automatic agent updates enabled (every 15 minutes)"
    else
        warn "Could not enable ${SYNCWIN_UPDATE_TIMER_NAME}; install it manually"
    fi
}

enroll_device() {
    info "Registering device with server..."
    local enroll_payload
    local host_name kernel_ver agent_ver
    host_name="$(hostname | tr -d '\n')"
    kernel_ver="$(uname -r | tr -d '\n')"
    agent_ver="$(get_installed_version | tr -d '\n')"
    enroll_payload=$(cat <<EOF
{
    "token": "${SYNCWIN_TOKEN}",
    "hostname": "${host_name}",
    "architecture": "${SYNCWIN_ARCH}",
    "os": "${SYNCWIN_DISTRO_ID}",
    "kernel": "${kernel_ver}",
    "agent_version": "${agent_ver}"
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
    local unit_file="/etc/systemd/system/${SYNCWIN_SERVICE_NAME}"
    local problems=0

    if [ -f "$unit_file" ] && ! grep -q "systemd-journal" "$unit_file"; then
        warn "The agent unit lacks 'systemd-journal'; the Logs screen will stay empty."
        warn "Adding it now."
        if sed -i 's/^SupplementaryGroups=.*/SupplementaryGroups=docker systemd-journal/' "$unit_file"; then
            systemctl daemon-reload
            systemctl restart "$SYNCWIN_SERVICE_NAME" >/dev/null 2>&1
            success "Added systemd-journal to the agent unit and restarted"
        else
            problems=$((problems + 1))
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
    step "8" "Verifying integrity..."
    verify_checksum
    step "9" "Installing binary and service..."
    install_binary
    install_systemd_service
    step "10" "Enrolling device and starting service..."
    enroll_device
    configure_service
    start_service
    install_auto_update
    verify_agent_readiness
    print_summary
}

main "$@"
