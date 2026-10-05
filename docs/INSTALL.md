# SyncWin Agent Installation

This document covers the SyncWin agent installer architecture, security model, and operational details.

## Architecture

The installer is a self-contained Bash script that:

1. Validates the target environment
2. Downloads the agent binary from the SyncWin server
3. Verifies binary integrity via SHA-256 checksum
4. Installs the binary and a hardened systemd service
5. Enrolls the device with the server using a temporary token
6. Starts the agent

```
Dashboard (authenticated)
    │
    │ POST /api/agent/enroll-token
    ▼
Server generates token (32 chars, 15min expiry, single-use)
    │
    ▼
Dashboard shows:
  curl -fsSL https://sync-win.local/install/TOKEN -o /tmp/sync-win-install.sh
  sudo bash /tmp/sync-win-install.sh
    │
    ▼
GET /install/TOKEN → install.sh (token embedded)
    │
    ▼
Install.sh:
  1. require_root
  2. detect_os
  3. detect_arch
  4. validate_environment
  5. check_dependencies
  6. create_user + configure group
  7. create_directories
  8. backup_existing
  9. download_binary
  10. verify_checksum
  11. install_binary
  12. install_systemd_service
  13. enroll_device
  14. configure_service
  15. start_service
```

## Installation

### From the Dashboard (preferred)

1. Open the SyncWin dashboard and sign in
2. Click "Show command" under "Add a Linux device"
3. Copy the command and run it on the target machine:

```bash
curl -fsSL https://sync-win.local/install/TOKEN -o /tmp/sync-win-install.sh
sudo bash /tmp/sync-win-install.sh
```

### Manual Installation

If you have the token and server URL:

```bash
sudo SYNCWIN_SERVER=https://sync-win.local SYNCWIN_TOKEN=xxxxx bash install.sh
```

### With Auto-Dependency Installation

```bash
sudo AUTO_INSTALL_DEPS=1 SYNCWIN_SERVER=https://sync-win.local SYNCWIN_TOKEN=xxxxx bash install.sh
```

## Security

### Enrollment Tokens

- **Temporary**: Valid for 15 minutes
- **Single-use**: Each token can only be consumed once
- **Server-bound**: Token maps to the owner's account server-side
- **No secrets in URLs**: Token is not the owner_id; it's a random 32-character string
- **Auto-expired**: Old tokens are cleaned up automatically

### Binary Integrity

- Installer downloads the binary to a temporary file
- SHA-256 checksum is verified before installation
- Download uses `--fail`, `--retry`, and `--connect-timeout`
- Corrupted downloads are detected and aborted

### Signed Auto-Updates

The checksum alone cannot prove *who* produced a binary, and the updater pulls
from the same server over HTTP. Auto-updates are therefore authenticated with
**Ed25519**:

1. Generate a keypair once (keep the private key secret):

   ```bash
   go run ./server/cmd/agentsign -genkey
   # private:<base64>   public:<base64>
   ```

2. Build the server image passing the private key as the BuildKit secret
   `agent_update_key`. The build derives the public key, embeds it in the agent
   (`-ldflags "-X main.agentUpdatePublicKey=..."`) and produces a detached
   signature served at `GET /api/agent/signature`.

   ```bash
   DOCKER_BUILDKIT=1 docker build \
     --secret id=agent_update_key,src=/path/to/private.key \
     -f docker/Dockerfile.server -t sync-win .
   ```

   For local builds, `make build-agent` accepts
   `AGENT_UPDATE_PUBLIC_KEY=<base64 public key>`.

3. At update time the agent downloads the binary and the signature **before**
   doing anything with them, verifies the signature, and only then installs.
   The downloaded binary is never executed before verification.

**Fail-closed:** an agent built without an embedded public key refuses to
auto-update. This is intentional — an unsigned update must never be installed.
CI signs automatically when the `AGENT_UPDATE_KEY` repository secret is set.

### Systemd Hardening

The service runs with the following protections:

| Directive | Purpose |
|-----------|---------|
| `NoNewPrivileges=yes` | Prevents privilege escalation via setuid/capabilities |
| `ProtectSystem=strict` | Mounts / and /boot as read-only |
| `ProtectHome=no` | Required to read user preference files from home directories |
| `PrivateTmp=yes` | Isolates /tmp namespace |
| `ProtectKernelTunables=yes` | Prevents modification of /proc, /sys |
| `ProtectKernelModules=yes` | Prevents kernel module loading |
| `ProtectControlGroups=yes` | Prevents cgroup modifications |
| `RestrictSUIDSGID=yes` | Prevents creation of SUID/SGID files |
| `RestrictRealtime=yes` | Prevents realtime scheduling |
| `RestrictNamespaces=yes` | Prevents namespace creation |
| `ReadWritePaths=/var/lib/sync-win /var/log/sync-win /etc/sync-win` | Limits write access |

### User Model

- Dedicated system user `sync-win` runs the service
- The installing user is added to the `sync-win` group for home directory read access
- No sudo or root required for normal operation
- `ProtectHome=no` is necessary because the agent reads user preference files

## Installed Structure

```
/usr/local/bin/sync-win-agent              # Agent binary

/etc/sync-win/                              # Configuration directory
    config.toml                        # Agent configuration (future)

/var/lib/sync-win/                          # Persistent data
    queue.db                           # Command queue
    state.db                           # Agent state

/var/log/sync-win/                          # Log directory

/etc/systemd/system/sync-win-agent.service  # Systemd unit
```

## Rollback

If any critical step fails after modifying the system:

1. **Binary installed but enrollment failed**: Binary is removed, service file is removed
2. **Service installed but start failed**: Service is stopped, binary is backed up
3. **Existing installation**: Previous binary is restored from backup

The rollback is automatic via the `trap rollback EXIT` mechanism.

## Idempotency

Running the installer again is safe:

- Detects existing installation
- Shows installed version vs available version
- Backs up existing binary before replacing
- Preserves device identity and configuration
- Does not duplicate systemd services

## Updating

To update the agent:

1. Generate a new install command from the dashboard
2. Run it on the target machine
3. The installer will:
   - Detect the existing installation
   - Back up the current binary
   - Download and install the new version
   - Re-enroll if needed (preserves device ID)
   - Restart the service

## Server API Endpoints

### `POST /api/agent/enroll-token`

**Authenticated** (requires the dashboard session cookie or a compatible Bearer token; cookie mutations also require CSRF).

Creates a temporary enrollment token.

**Response:**
```json
{
  "token": "abcdefghijklmnopqrstuvwxyz012345"
}
```

### `GET /install/{token}`

**Public.**

Returns the install script with the token embedded. Validates token before serving.

### `POST /api/agent/enroll`

**Public.**

Exchanges an enrollment token for device identity.

**Request:**
```json
{
  "token": "abcdefghijklmnopqrstuvwxyz012345",
  "hostname": "my-workstation",
  "architecture": "amd64",
  "os": "arch",
  "kernel": "6.10.0-arch1-1",
  "agent_version": "0.5.1"
}
```

**Response:**
```json
{
  "device_id": "dev-1",
  "device_token": "abc123def456"
}
```

### `GET /api/agent/checksums`

**Public.**

Returns the SHA-256 checksums file for binary verification.

### `GET /api/agent/signature`

**Public.**

Returns the base64 Ed25519 signature of the agent binary (served from
`SYNCWIN_AGENT_SIGNATURE`, default `/app/sync-win-agent.sig`). Returns `404` when the
image was built without a signing key; the agent then refuses to auto-update.

## Generating Install Commands

### From the Dashboard

The dashboard automatically generates the install command when you click "Show command".

### Programmatic

```bash
# 1. Create enrollment token (authenticated)
TOKEN=$(curl -s -X POST \
  -H "Authorization: Bearer YOUR_SESSION_TOKEN" \
  https://sync-win.local/api/agent/enroll-token | jq -r '.token')

# 2. Generate command
echo "curl -fsSL https://sync-win.local/install/$TOKEN -o /tmp/sync-win-install.sh && sudo bash /tmp/sync-win-install.sh"
```

## Troubleshooting

### "SYNCWIN_SERVER is not set"

The installer couldn't determine the server URL. Set it manually:

```bash
sudo SYNCWIN_SERVER=https://sync-win.local SYNCWIN_TOKEN=xxxxx bash install.sh
```

### "Enrollment token expired"

Tokens are valid for 15 minutes. Generate a new one from the dashboard.

### "Checksum mismatch"

The downloaded binary may be corrupted. Retry the installation.

### Service not starting

```bash
# Check service status
sudo systemctl status sync-win-agent.service

# Check logs
sudo journalctl -u sync-win-agent.service -n 50

# Common issues:
# - Server unreachable from the target machine
# - Firewall blocking the connection
# - Invalid device credentials
```

### Permission denied

The installer must run as root:

```bash
sudo bash install.sh
```

## Distro Support

The installer automatically detects the OS via `/etc/os-release` and supports:

- Arch Linux / Manjaro / EndeavourOS
- Ubuntu / Debian / Linux Mint / Pop!_OS
- Fedora
- RHEL / Rocky Linux / AlmaLinux / CentOS
- openSUSE / SLES

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `SYNCWIN_SERVER` | (required) | Server URL |
| `SYNCWIN_TOKEN` | (required) | Enrollment token |
| `SYNCWIN_USER` | `sync-win` | System user for the service |
| `SYNCWIN_GROUP` | `sync-win` | Group for home directory access |
| `AUTO_INSTALL_DEPS` | `0` | Set to `1` to auto-install dependencies |
