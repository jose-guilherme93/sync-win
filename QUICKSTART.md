# Quick Start

This guide walks through setting up SyncWin from scratch: starting the server, signing in as admin, and installing the agent on a Linux device.

## Prerequisites

- Docker with the Compose plugin, on the machine that will run the server
- A Linux machine for running the agent
- Both machines must be network-reachable (same LAN, or with port 8080 forwarded)

Nothing else. No checkout, no Go, no Node — the published image is used.

## 1. Start the server

Copy the compose file and generate the one required secret:

```bash
curl -fsSLO https://raw.githubusercontent.com/jose-guilherme93/sync-win/main/compose.yaml
echo "SYNCWIN_SECRET_KEY=$(openssl rand -hex 32)" > .env
docker compose up -d
```

The server starts at `http://localhost:8080`. Data is stored in the
`sync-win-data` Docker volume, so it survives updates.

`SYNCWIN_SECRET_KEY` is required and compose refuses to start without it, with a
message naming the exact command above. Keep it safe and stable: it encrypts
stored notification credentials, and changing it makes them unreadable.

### Building from a checkout instead

```bash
git clone https://github.com/jose-guilherme93/sync-win.git
cd sync-win
echo "SYNCWIN_SECRET_KEY=$(openssl rand -hex 32)" > .env
docker compose up -d --build
```

`make prod` does the same through the Makefile, and `make env` creates the `.env`
for you.

### Working on the code

For hot reload instead of a production image:

```bash
make dev     # dashboard http://localhost:5173 · api http://localhost:8088
```

Run `make help` for the full target list.

## 2. Create your account

The quick-start `compose.yaml` leaves registration **open** so you can sign up
from the browser on first run:

1. Open `http://localhost:8080`
2. Choose **Create account** and pick an email and password (at least 8
   characters)

**Then close it again.** Edit `.env`:

```bash
SYNCWIN_ENABLE_REGISTRATION=false
```

and apply it with `docker compose up -d`. While it stays open, anyone who can
reach the server can create an account on your instance.

### Prefer to bootstrap the first account instead

Set these in `.env` before the first start and no sign-up is needed:

```bash
SYNCWIN_ENABLE_REGISTRATION=false
SYNCWIN_ADMIN_EMAIL=you@example.com
SYNCWIN_ADMIN_PASSWORD=at-least-8-chars
```

The account is created on boot only if it does not exist. The password in `.env`
is **never** used to overwrite an existing account, so a password changed in the
dashboard survives restarts.

Accounts are stored in SQLite, at `sync-win.db` inside the `sync-win-data`
volume (`./data-dev/sync-win.db` in development).

### Development shortcut

The dev stack (`make dev`) also enables registration:

```bash
make dev-account   # creates demo@sync-win.local / sync-win-demo-password on :8088
```

Or register through the API directly (only works when registration is enabled):

```bash
curl -X POST http://localhost:8080/api/auth/register \
  -H 'Content-Type: application/json' \
  -d '{"email":"you@example.com","password":"at-least-8-chars"}'
```

The automated test suites (`make test`, `make test-race`, `npm run test`) need no
account at all — they mock the API.

## 3. Install the agent on a Linux machine

### From the dashboard (recommended)

1. In the dashboard, click the **Install Agent** button (or navigate to the install page)
2. The server generates a one-time enrollment token
3. Copy the displayed command and run it on your Linux machine:

```bash
curl -fsSL https://YOUR-SERVER:8080/install/TOKEN -o /tmp/sync-win-install.sh
sudo bash /tmp/sync-win-install.sh
```

The installer will:
- Detect your OS and architecture
- Download the agent binary from the server
- Verify the SHA-256 checksum
- Install the binary and a hardened systemd user service
- Enroll the device with the server
- Start the agent

On success, the device appears on your dashboard within seconds.

### Manual installation

If you prefer to install manually, see [docs/INSTALL.md](docs/INSTALL.md) for the full installation architecture and security model.

## 4. Verify the installation

On the Linux machine:

```bash
# Check agent status
sudo systemctl status sync-win-agent

# View agent logs
sudo journalctl -u sync-win-agent -f
```

On the dashboard:
- The device should appear with status "Online"
- Hardware telemetry should start flowing within 10 seconds
- Preference files should sync on the next cycle (default: 300 seconds)
- The **Logs** tab should fill within about a minute

## 5. Configure save-game sync (optional)

By default, the agent collects saves from Hydra Launcher, Steam, and Unity3D. To add extra save directories:

1. Open the dashboard Settings modal (gear icon)
2. Add extra directories under "Save-game folders"
3. Click Save

The agent will pick up the new directories on its next sync cycle.

## 6. Set up notifications (optional)

1. Open the Notifications modal (bell icon)
2. Configure a notification provider:
   - **Web inbox**: no setup needed, events appear in the dashboard
   - **Telegram**: enter your bot token and chat ID
   - **Webhook**: enter the target URL
3. Select which events to subscribe to (device offline, online, sync error, enrolled)
4. Click Save

## 7. Docker management (optional)

If Docker is running on your Linux devices:

1. Open the device detail modal
2. Click the **Docker** tab
3. You can now:
   - View running containers
   - Start/stop/restart/kill/remove containers
   - Execute commands inside containers
   - View and edit compose files
   - Prune images, containers, and networks

## Architecture overview

```
Dashboard (browser)
    │
    │ HTTP REST API
    ▼
Server (Docker: Go + SQLite)
    │
    │ Command queue (polling)
    ▼
Agent (Linux: Go binary)
    │
    │ Local execution
    ▼
Files, Telemetry, Docker
```

- **Server** is passive: it never executes commands on clients directly.
- **Agent** pulls commands and executes them locally.
- **Dashboard** communicates only with the server.

## Troubleshooting

### Agent cannot connect

1. Check if the server is reachable: `curl http://YOUR-SERVER:8080/health`
2. Check firewall: port 8080 must be open
3. Check agent logs: `sudo journalctl -u sync-win-agent -n 50`
4. If using Docker Compose with `network: host`, the server is directly on the host network

### Device shows "Offline"

- The agent sends heartbeats every 10 seconds
- Status derivation: online (<30s), stale (>30s), offline (>5min)
- Check agent logs for connection errors
- Restart the agent: `sudo systemctl restart sync-win-agent`

### Logs tab is empty for every device

The agent can only read its own journal entries unless it belongs to the
`systemd-journal` group, because journal files are `0640 root:systemd-journal`.
Check whether the running agent has it:

```bash
grep Groups /proc/$(pidof sync-win-agent)/status
```

If `systemd-journal` is missing, install the group and restart:

```bash
sudo usermod -aG systemd-journal sync-win
sudo systemctl restart sync-win-agent
```

Re-running the installer does this for you. The dashboard also shows the agent's
own explanation when log collection fails, at the top of the Logs screen.

### Preference files not syncing

- Check the agent logs for file collection errors
- Verify files are in the allowlist (check `~/.config/sync-win/allowed-files`)
- Check file sizes: text files must be under 256 KiB, saves under 1 MiB
- Verify no secrets are in the file content (agent rejects files with secret patterns)

### Docker tab empty

- Verify Docker is running on the device: `docker ps`
- Check the agent has access to `/var/run/docker.sock`
- The agent checks `DockerIsAvailable()` before reporting Docker data

## Next steps

- Read [ARCHITECTURE.md](ARCHITECTURE.md) for the full system design
- Read [SECURITY.md](SECURITY.md) for security rules and best practices
- Read [API.md](API.md) for the complete REST API reference
- Read [COLLECTION-CONTRACT.md](COLLECTION-CONTRACT.md) for the agent-server contract
