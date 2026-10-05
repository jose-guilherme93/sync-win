# SyncWin

SyncWin is a lightweight self-hosted system for synchronizing small user preference files and game save data from Linux devices to a central Docker server. The main goal is to track the preferences of each device and show the current status in the web UI: online devices, last sync time, live hardware telemetry, installed packages, and stored preference/save data.

## Problem it solves

Users often want a simple way to keep per-device preferences organized and visible across Linux machines without building a full environment backup system. Typical needs include:

- knowing which devices are online
- tracking the last synchronization time
- storing only small text preference files
- keeping a minimal record of user configuration state
- avoiding the complexity of full snapshots or broad home backups
- syncing game save files from Hydra Launcher (Wine prefixes, Steam, Unity) and other launchers
- monitoring hardware health (CPU, memory, disk, network, temperature)
- viewing installed applications across devices
- managing Docker containers remotely
- receiving notifications about device status changes

This project solves that by syncing only explicit, small preference files and selected save-game data to the server.

## Architecture

The project is split into three main layers:

- **Server**: Go + SQLite REST API running in Docker. Receives device metadata, stores preference files, manages user accounts, dispatches notifications, and serves the web dashboard.
- **Agent**: Lightweight Go binary installed on each Linux machine. Collects preference files, saves, telemetry, and installed apps. Executes Docker management commands locally.
- **Web**: Svelte 5 + TypeScript 6 dashboard showing online devices, synchronization timestamps, hardware telemetry charts, installed packages, Docker container management, and notification settings.

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full design.

## Server

The server is a Go application that runs in Docker. It exposes a REST API, stores state in SQLite with WAL journal mode, and keeps small text files and base64-encoded saves in a data directory. It serves the web dashboard from the same origin (no CORS). Features include:

- User authentication (register, login, HttpOnly session cookie with CSRF protection)
- Device registry with heartbeat tracking and online/offline status
- Preference file storage (text + base64-encoded saves)
- Docker command queue management (proxy to agents)
- Notification dispatch (Telegram, webhook, web inbox) with encrypted credentials
- Device notes and file attachments
- Structured JSON logging with secret redaction
- Telemetry history with downsampled aggregation

## Agent

The agent is a lightweight Go binary (zero external dependencies) installed on each Linux machine via a one-line installer. It collects selected preference files and game saves, uploads them to the server, and reports device heartbeat, hardware telemetry, and installed packages. Features include:

- Allowlist-based file collection (no broad filesystem scans)
- Game save collection from Hydra Launcher, Steam, Unity3D, and operator-configured directories
- Binary saves encoded as base64 with per-file (1 MiB) and per-cycle (16 MiB) budgets
- Hardware telemetry (CPU, memory, disk I/O, network, temperatures, battery, processes)
- Docker container monitoring and management (21 command types)
- Installed application inventory (apt, flatpak, pacman, AUR, AppImages)
- Local policy enforcement (any command type can be disabled)
- Exponential backoff with jitter for server outages
- Fresh enrollment-token recovery when device credentials are lost (hardware fingerprints are never used for authentication)

Everything the agent collects and executes is defined in an explicit written contract (`COLLECTION-CONTRACT.md`, machine-readable at `agent/internal/contract/contract.json`). The agent owns command execution: a local policy file (`~/.config/sync-win/policy.json`) can disable any command type, and refusals are reported back to the server. Commands run with hard timeouts and output caps; sync survives server outages via exponential backoff with jitter.

The one-line installer prints numbered progress steps and verifies the connection to the server before declaring success; on failure it prints an actionable checklist (reachability, LAN IP vs container IP, firewall, live logs). Devices are registered at install time, so they appear on the dashboard within seconds — bound to the account that generated the command. The agent binary path served by the server defaults to `/app/sync-win-agent` and can be overridden with `SYNCWIN_AGENT_BINARY`.

## Web dashboard

The web frontend is a Svelte 5 + TypeScript 6 + Vite 8 application that shows devices online/offline, last sync information, live hardware telemetry charts, installed packages, and synchronized preference files. Features include:

- Account-first auth (sign in; public registration is disabled by default and the first account is bootstrapped from `SYNCWIN_ADMIN_EMAIL`/`SYNCWIN_ADMIN_PASSWORD`)
- Device grid with status badges and save counts
- Real-time CPU/memory/network sparklines on device cards (lightweight canvas)
- Device detail modal with tabs: System, Files, Packages, Saves, Notes, Docker
- Docker container management (start/stop/restart/kill/remove, exec, compose editor, prune)
- Telemetry history with time period selection
- Notification settings and inbox with real-time SSE push
- Device notes and file attachments

**Save-game view**: each device card shows a badge with save count, total size, and last sync time. The Settings modal (gear icon) lists built-in Hydra/Steam/Unity locations and lets operators add extra save folders. The device detail tabs include a "saves" tab listing all synced save files with their sizes and timestamps; binary files are marked and viewable as base64.

In production the server serves the built dashboard itself from `SYNCWIN_WEB_DIR` (default `/app/web`, baked into the Docker image), so everything runs on a single origin and port. For UI development use `npm run dev` (Vite) pointed at a running server; large app inventories render in capped groups with search to keep interactions smooth.

## Getting started

### Quick start with Docker Compose

```bash
make prod
```

The production stack builds the image and starts the server at `http://localhost:8080` with persistent data in the `sync-win-data` Docker volume (set `SYNCWIN_DATA_PATH=./data` for a host bind mount). Public registration is disabled by default; set `SYNCWIN_ADMIN_EMAIL` and `SYNCWIN_ADMIN_PASSWORD` in `.env` to create the first account, then sign in and install the agent on your Linux devices.

Before the first production run, create the environment file with a generated secret:

```bash
make env      # creates .env from .env.example and fills SYNCWIN_SECRET_KEY
# then set SYNCWIN_ADMIN_EMAIL and SYNCWIN_ADMIN_PASSWORD in .env
make prod
```

`make help` lists every target. If you prefer raw Compose, `docker compose -f compose.prod.yaml up -d --build` is equivalent.

### Development environment

```bash
make dev
```

This brings up the whole application in containers with hot reload:

| Service | URL | Behaviour |
| --- | --- | --- |
| Server | `http://localhost:8088` | Go server rebuilt by [air](https://github.com/air-verse/air) on every change |
| Dashboard | `http://localhost:5173` | Vite dev server with HMR |

Development data lives in `./data-dev`, separate from production `./data`. To run dev alongside a running production stack, publish dev on other ports:

```bash
DEV_HTTP_PORT=8081 WEB_PORT=5199 make dev
```

The Vite dev server automatically points at the dev API port via `VITE_API_BASE`.

Useful targets: `make dev-d` (detached), `make dev-logs`, `make dev-down`, `make clean-dev`.

### Agent installation

From the dashboard, generate an enrollment token and run the one-line installer on your Linux machine:

```bash
curl -fsSL https://sync-win.local/install/TOKEN -o /tmp/sync-win-install.sh
sudo bash /tmp/sync-win-install.sh
```

See [QUICKSTART.md](QUICKSTART.md) for detailed instructions.

## Running from source

The `Makefile` wraps the common commands:

```bash
make build        # server + agent + web
make test         # go test (server, agent) + svelte-check
make lint         # go vet + svelte-check
make run-server   # go run the API on :8080 against ./data-dev
make run-web      # Vite dev server on :5173
make run-agent    # run the agent daemon locally
```

Individual builds remain available:

```bash
cd server && go build ./...
cd ../agent && go build ./...
cd ../web && npm install && npm run build
```

### Debug logs

The server logs every HTTP request as a structured JSON line, including method, path, status, duration, and remote address. Authentication events are also logged without passwords.

Follow live container logs:

```bash
docker compose logs -f server
```

The same log stream is persisted at `/data/sync-win-server.log` inside the `sync-win-data` volume:

```bash
docker run --rm -v sync-win-data:/data alpine tail -f /data/sync-win-server.log
```

### Agent logs on Linux

The agent is installed as a user service named `sync-win-agent.service`, not as a system-wide service. Use the `--user` flag for every systemd command:

```bash
systemctl --user status sync-win-agent.service
journalctl --user -u sync-win-agent.service -f
```

Useful variations:

```bash
# Last 100 entries
journalctl --user -u sync-win-agent.service -n 100 --no-pager

# Only logs from the current boot
journalctl --user -u sync-win-agent.service -b --no-pager

# Restart after updating the agent
systemctl --user restart sync-win-agent.service

# Show the installed unit and executable
systemctl --user cat sync-win-agent.service
```

The unit file is stored at:

```text
~/.config/systemd/user/sync-win-agent.service
```

### Agent updates

New installations enable `sync-win-agent-update.timer` by default. It checks the server every 15 minutes, compares versions, verifies the SHA-256 checksum and the downloaded binary version, then replaces the agent atomically and restarts it. Failed health checks roll back automatically.

Disable it before installation with:

```bash
sudo SYNCWIN_AUTO_UPDATE=0 bash install.sh
```

The updater is pull-based: the agent only contacts the configured SyncWin server. The system-wide installer uses a root systemd timer; the user installer uses `systemctl --user`.

## Releases

Releases are automatic. Every push to `main` creates the next semantic version
from [Conventional Commits](https://www.conventionalcommits.org/):

| Commit | Bump | Example |
| --- | --- | --- |
| `fix:`, `chore:`, anything else | patch | `0.2.0` → `0.2.1` |
| `feat:` | minor | `0.2.1` → `0.3.0` |
| `feat!:`, `BREAKING CHANGE:` | major | `0.3.0` → `1.0.0` |

The workflow tags the commit (`vX.Y.Z`), creates the GitHub Release and, when
`DOCKERHUB_USERNAME`/`DOCKERHUB_TOKEN` are configured, publishes the Docker
image. Manual runs can force a bump from the Actions tab.

## Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) — full architectural design
- [DATA-MODEL.md](DATA-MODEL.md) — conceptual data model
- [SNAPSHOT-FORMAT.md](SNAPSHOT-FORMAT.md) — preference sync format specification
- [COLLECTION-CONTRACT.md](COLLECTION-CONTRACT.md) — binding contract between agent and server
- [AGENTS.md](AGENTS.md) — AI agent development guide
- [ROADMAP.md](ROADMAP.md) — development roadmap
- [QUICKSTART.md](QUICKSTART.md) — quick start guide
- [SECURITY.md](SECURITY.md) — security model and rules
- [API.md](API.md) — REST API reference
- [docs/INSTALL.md](docs/INSTALL.md) — agent installation architecture
