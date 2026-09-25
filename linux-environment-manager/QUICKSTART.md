# Quick Start

This guide walks through setting up LEM from scratch: starting the server, creating an account, and installing the agent on a Linux device.

## Prerequisites

- Docker and Docker Compose installed on the server machine
- A Linux machine for running the agent
- Both machines must be network-reachable (same LAN or with port 8080 forwarded)

## 1. Start the server

From the repository root:

```bash
make env     # create .env with a generated LEM_SECRET_KEY
make prod    # build the image and start the stack
```

The server starts at `http://localhost:8080`. The first build takes a few minutes (Go + Node multi-stage). Data is stored in `./data`.

To hack on the UI or API instead, use the development stack with hot reload:

```bash
make dev     # dashboard http://localhost:5173 · api http://localhost:8088
```

Run `make help` for the full target list.

## 2. Create an account

Open `http://localhost:8080` in your browser. You will see the sign in / create account screen.

1. Click **Create account**
2. Enter your email and password
3. Click **Register**

You are now logged in and on the main dashboard (empty, since no devices are enrolled yet).

## 3. Install the agent on a Linux machine

### From the dashboard (recommended)

1. In the dashboard, click the **Install Agent** button (or navigate to the install page)
2. The server generates a one-time enrollment token
3. Copy the displayed command and run it on your Linux machine:

```bash
curl -fsSL https://YOUR-SERVER:8080/install/TOKEN -o /tmp/lem-install.sh
sudo bash /tmp/lem-install.sh
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
systemctl --user status lem-agent.service

# View agent logs
journalctl --user -u lem-agent.service -f
```

On the dashboard:
- The device should appear with status "Online"
- Hardware telemetry should start flowing within 10 seconds
- Preference files should sync on the next cycle (default: 300 seconds)

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
3. Check agent logs: `journalctl --user -u lem-agent.service -n 50`
4. If using Docker Compose with `network: host`, the server is directly on the host network

### Device shows "Offline"

- The agent sends heartbeats every 10 seconds
- Status derivation: online (<30s), stale (>30s), offline (>5min)
- Check agent logs for connection errors
- Restart the agent: `systemctl --user restart lem-agent.service`

### Preference files not syncing

- Check the agent logs for file collection errors
- Verify files are in the allowlist (check `~/.config/lem/allowed-files`)
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
