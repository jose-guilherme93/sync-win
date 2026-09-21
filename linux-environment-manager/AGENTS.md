# AGENTS.md

This file is the official guide for AI agents working on Linux Environment Manager (LEM).

## Objective

Linux Environment Manager is a self-hosted system for synchronizing small user preference files and game save data from Linux devices to a central Docker server. The web dashboard shows online devices, last sync timestamps, live hardware telemetry (CPU, memory, disk, network), installed packages, and synced preference/save files. The system is intentionally lightweight and does not treat the server as a full environment snapshot store.

## Architectural summary

The project is built around three main components:

1. Server
   - Dockerized
   - Go + REST API with persistent SQLite state under /data (WAL journal mode, embedded schema migrations)
   - stores small preference files and sync metadata
   - serves the web dashboard from the same origin (no CORS)
   - proxies Docker management commands to agents via command queue
   - manages user accounts with session-based auth (Bearer tokens)
   - dispatches notifications via pluggable providers (Telegram, webhook, inbox)
   - never executes arbitrary commands on clients

2. Agent
   - runs locally on each Linux machine
   - collects small explicit preference files and game saves
   - reports device heartbeat, hardware telemetry, and installed packages
   - monitors Docker containers via local socket
   - executes Docker management commands (start, stop, restart, kill, remove, exec, compose, prune)
   - enforces local policy: any command type can be disabled via `~/.config/lem/policy.json`
   - survives server outages via exponential backoff with jitter
   - persists state across restarts (`~/.local/state/lem/agent-state.json`)

3. Web dashboard
   - Svelte 5 + TypeScript 6 + Vite 8 + Chart.js 4
   - account-first: sign in / create account entry screen
   - shows online devices, last sync time, live hardware telemetry charts
   - device detail modal with tabs: System, Files, Packages, Saves, Notes, Docker, Security
   - Docker container management (list, start/stop/restart/kill/remove, exec, compose, prune)
   - Lynis security audit: run audit, view hardening index, warnings/suggestions, history
   - notification settings and inbox with real-time SSE push
   - device notes and attachments

## Repository structure

```text
linux-environment-manager/
├── server/
│   ├── cmd/server/main.go          # Entry point
│   ├── internal/
│   │   ├── app/app.go              # All HTTP handlers and routes (~2,750 lines)
│   │   ├── store/                  # SQLite CRUD, aggregation, schema (initSchema)
│   │   ├── logging/                # Structured JSON logger, redaction, dedup
│   │   ├── notify/                 # Notification providers (Telegram, webhook, inbox)
│   │   ├── crypto/                 # AES-GCM encryption for provider configs
│   │   └── docker/                 # Docker command queue and broadcaster
│   ├── migrations/                 # Historical SQL files (schema is applied by store.initSchema)
│   └── lem-agent.service           # Systemd unit template
├── agent/
│   ├── cmd/agent/main.go           # Entry point (CLI daemon mode)
│   ├── collectors/                 # 13 hardware/Docker collectors
│   │   ├── battery.go, cpu_cores.go, disk_partitions.go
│   │   ├── docker.go               # Docker socket reader (21 operations)
│   │   ├── hardware_id.go          # Hardware fingerprint for device reconnection
│   │   ├── memory_expanded.go, network.go, thermal.go
│   │   ├── top_processes.go, logs.go, agent_impact.go
│   │   └── collectors.go           # Base interface and types
│   └── internal/
│       ├── bootstrap/bootstrap.go  # Agent initialization
│       └── contract/               # Machine-readable contract (contract.json + Go loader)
│           ├── contract.json       # Single source of truth for all limits and allowed paths
│           └── contract.go         # Embedded loader with runtime validation
├── web/
│   ├── src/
│   │   ├── App.svelte              # Root: auth, dashboard, polling
│   │   ├── components/
│   │   │   ├── DeviceModal.svelte  # Device detail (System/Files/Packages/Saves/Notes/Docker)
│   │   │   ├── DockerTab.svelte    # Docker container management
│   │   │   ├── Sparkline.svelte       # Lightweight canvas sparklines for device cards
│   │   │   ├── SystemMetrics.svelte    # Detailed hardware telemetry
│   │   │   ├── SimpleMetrics.svelte    # Device summary cards
│   │   │   ├── HistoryModal.svelte     # Telemetry history
│   │   │   ├── NotificationsModal.svelte  # Notification settings and inbox
│   │   │   └── NotificationToast.svelte   # Toast notifications
│   │   └── lib/
│   │       ├── telemetry-store.ts  # Telemetry state management
│   │       └── telemetry-cache.ts  # 60s TTL cache
│   └── dist/                       # Production build (served by Go server)
├── docker/
│   ├── Dockerfile.server           # Multi-stage: Go + Node -> Alpine
│   ├── Dockerfile.server.dev       # Dev server with air hot-reload
│   └── Dockerfile.web.dev          # Vite dev server (HMR)
├── scripts/
│   └── install.sh                  # Agent installer (616 lines, idempotent)
├── docs/
│   └── INSTALL.md                  # Installation architecture and security
├── data/                           # Runtime: SQLite DB + mirrored preference files
├── data-dev/                       # Runtime for the dev stack (gitignored)
├── Makefile                        # make dev / make prod / make test / make help
├── compose.yaml                    # Simple single-server stack
├── compose.dev.yaml                # Dev stack: server (hot-reload) + web (HMR)
├── compose.prod.yaml               # Production stack (built image, ./data)
├── AGENTS.md
├── ARCHITECTURE.md
├── COLLECTION-CONTRACT.md
├── DATA-MODEL.md
├── SNAPSHOT-FORMAT.md
├── README.md
├── ROADMAP.md
└── SECURITY.md
```

## How to develop

- Keep changes small and explicit.
- Prefer minimal features over speculative architecture.
- Update documentation whenever the product direction changes.
- Keep the server safe and lightweight.
- Keep the agent focused on explicit file collection and sync operations.
- The contract (`COLLECTION-CONTRACT.md` + `agent/internal/contract/contract.json`) is the single source of truth for what the agent collects and sends.
- Use the Makefile for environment work: `make dev` (containers, server hot-reload + web HMR, data in `data-dev/`), `make prod` (containers, built image, data in `data/`), `make help` for the rest. `compose.yaml` is the simple single-server stack.

## How to run tests

From the repository root:

```bash
make test          # server + agent go test, web svelte-check
make lint          # go vet + svelte-check
```

Or the underlying commands directly:

```bash
cd server && go test ./...
cd ../agent && go test ./...
cd ../web && npm install && npm run build
```

Use the most direct command for the module being changed.

## How to add collectors

- Place new collectors under `agent/collectors`.
- Each collector must target a small, explicit file set.
- Do not scan broad parts of the home directory.
- Favor allowlists and file-type filters.
- Exclude tokens, secrets, flat secret stores, and credentials by default.
- Add tests that validate filtered paths and allowed file types.

## How to add Docker collectors

- Docker collectors live in `agent/collectors/docker.go`.
- They communicate with the Docker Engine via the UNIX socket (`/var/run/docker.sock`) using HTTP over `net.Dial("unix", ...)`.
- All container IDs must be validated against injection (`validContainerID()`).
- Docker exec commands must have a 30s timeout and 64KB output cap.
- Compose file writes must be restricted to valid compose filenames.
- All Docker operations should check `DockerIsAvailable()` before execution.
- Docker data queries return structured JSON in the command message field.

## How to add preference sync logic

- Place sync logic in the agent or the server API layer.
- Keep the payload small and text-based.
- Store user data only when it is explicitly allowed.
- Keep sync status and last sync timestamps in the model.
- Add tests around online/offline state and file filtering.

## How to add notification providers

- Place new providers under `server/internal/notify`.
- Each provider implements the `Provider` interface: `Name()`, `Label()`, `Description()`, `PublicFields()`, `Validate()`, `Send()`, `Test()`.
- Register via `init()` to auto-register on import.
- Add provider tests in `notify_test.go` using the `testProvider` helper.
- Provider credentials must be encrypted with `crypto.EncryptConfig` before storage.
- Secrets are never returned to the frontend; `PublicFields()` declares which fields are safe to expose.
- Keep the dispatch fan-out non-blocking: provider failures never block other providers or the inbox.

## Security rules

- Never execute arbitrary shell commands remotely.
- Never store secrets in plaintext.
- Never copy the entire home directory.
- Never include passwords, KWallet, tokens, keys, or browser credentials.
- Never accept oversized or binary data by default.
- Never perform destructive actions without confirmation and a clear safety check.

## Performance rules

- Prefer allowlists that read only small selected files.
- Avoid full filesystem scans.
- Keep idle memory usage low.
- Use small payloads and short sync intervals.
- Store only what is needed for the dashboard and user preference view.

## Compatibility rules

- Linux is the target platform.
- KDE is the first supported desktop, but the design should remain extensible.
- The initial system should support typical Linux desktop environments without requiring broad full-machine processing.

## Architecture rules

- Do not alter the architecture without updating documentation.
- Do not add snapshot or restore features unless the product definition changes again.
- Keep server and agent responsibilities clearly separated.
- Prefer small text files and explicit metadata over large dynamic models.

## Testing rules

- Add tests for new file collectors and sync logic.
- Validate device online state, last sync timestamps, and filtering behavior.
- Keep tests focused on real behavior rather than mock-only assertions.

## Dependency rules

- Do not add dependencies without a strong reason.
- Prefer the standard library and minimal runtime features.
- Keep the project small and deliberate.

## Data handling rules

- Do not store secrets.
- Ignore sensitive paths by default.
- Do not collect the entire `~/.config` tree automatically.
- Prefer explicit allowlists and selected application paths.
- Only store small text files as the main persistent payload.

## AI workflow rule

When working in this repository, do not bypass required documentation and contract updates. If a change affects the architecture or product direction, it must be reflected in the project docs and roadmap.
