# Architecture

## Overview

SyncWin is a self-hosted system for synchronizing small user preference files and game save data from Linux devices to a central Docker server. The web dashboard shows online devices, last sync timestamps, live hardware telemetry (CPU, memory, disk, network), installed packages, and synced preference/save files. The system is intentionally lightweight and does not treat the server as a full environment snapshot store.

The architecture is agent-first. The server is intentionally minimal and never executes arbitrary commands on endpoints. Each Linux machine runs an agent that collects small, explicit preference files and pushes them to the server.

## Components

### 1. Server

The server runs in Docker and exposes a REST API over HTTP. It stores persistent state in SQLite with WAL journal mode and serves the web dashboard from the same origin. It is responsible for:

- registering devices and managing enrollment tokens
- tracking online/offline state via heartbeat
- storing per-device user preference files (text and base64-encoded saves)
- recording last synchronization timestamps
- receiving authenticated hardware telemetry from agents
- serving the web dashboard (production build from `SYNCWIN_WEB_DIR`, default `/app/web`)
- exposing health and basic diagnostics
- proxying Docker management commands to agents via a command queue
- managing user accounts with session-based auth (HttpOnly cookie for the dashboard, bearer compatibility for API clients)
- dispatching notifications via pluggable providers (Telegram, webhook, web inbox)
- storing device notes and file attachments
- structured JSON logging with secret redaction and deduplication
- telemetry history with downsampled aggregation

The server does not execute remote commands on client machines. Docker management is proxied through the agent command queue: the server stores a command, the agent picks it up, executes it against the local Docker socket, and reports the result back.

### 2. Agent

The agent runs on each Linux device. It is the only component allowed to touch the local machine. It is responsible for:

- collecting user preference files from an explicit allowlist
- collecting game save files from Hydra Launcher, Steam, Unity3D, and operator-configured extra directories
- syncing selected text and binary files to the server (binary saves are base64-encoded)
- recording sync metadata and content hashes (unchanged files are not re-uploaded)
- reporting device status and heartbeat
- reading Linux `/proc` telemetry for CPU, memory, disk I/O, uptime, load, and system identity
- collecting CPU core usage, temperatures, battery, network interfaces, disk partitions, swap, top processes
- measuring its own resource impact (agent CPU and memory usage)
- monitoring Docker containers via the local Docker socket (`/var/run/docker.sock`)
- executing Docker management commands (start, stop, restart, kill, remove, exec, compose read/write/up/down/ps/logs, system/image/container/network prune)
- reporting Docker container status and engine info in telemetry
- collecting installed application inventory (apt, flatpak, pacman, AUR, AppImages)
- reconnecting to the server only through a fresh enrollment flow after credential loss
- enforcing local policy: any command type can be disabled via `/etc/sync-win/policy.json`
- interactive remote access (opt-in, off by default): keeps an outbound tunnel, and for each session injects an ephemeral key through a root helper and runs `ssh` against its own loopback
- surviving server outages via exponential backoff with jitter
- persisting state across restarts (`~/.local/state/sync-win/agent-state.json`)

### 3. Web Dashboard

The web dashboard is a Svelte 5 + TypeScript 6 + Vite 8 application. It shows the devices that are online, their last sync time, their metadata, live hardware telemetry charts, installed packages, and the small files associated with them. It also provides Docker container management and Lynis security audit results.

The dashboard shell is **navigation-first**: a fixed left sidebar (collapsible, drawer on mobile) owns every route, and the selected device scopes the Hardware / System / Customize groups. Selecting a device changes the main panel context — it does not open a modal. A topbar carries the device identity, online status, last-seen time, a refresh-interval selector, global search and the notification/account popovers.

The dashboard is **account-first**: the entry screen is sign in / create account, every device is owned by an account, and agents are registered under the account that generated the install command. Authorization always comes from a valid server session; anonymous identity headers and owner query parameters are ignored.

The visual language lives in `lib/theme.css` (design tokens: near-black surfaces, a single mint accent for healthy/selected state, amber/red/grey for status, separate chart series colours). Components read from these variables rather than hardcoding colours.

Key UI components:
- `App.svelte`: shell wiring, auth gate, polling, and the popovers anchored under the topbar
- `lib/router.ts`: navigation store and the `NAV_GROUPS` structure the sidebar renders
- `lib/types.ts`: shared `Device` / `HardwareStats` shapes and status helpers
- `lib/insights.ts`: alert thresholds and `generateInsights`, the single definition of what needs attention
- `components/shell/Sidebar.svelte`, `Topbar.svelte`, `DeviceList.svelte`: the app chrome
- `components/ui/`: reusable primitives (Icon, StatusDot, SeverityBadge, GaugeCard, StatCard, MiniSparkline, EmptyState, Skeleton, ConfirmDialog)
- `components/screens/Home.svelte`: fleet KPI row, device table with sparklines, recent-alerts feed
- `components/screens/DeviceOverview.svelte`: gauges, per-core load, storage/network cards, history chart
- `components/screens/Alerts.svelte`, `Reports.svelte`: fleet-wide findings and inventory reports
- `components/screens/Storage.svelte`, `Processes.svelte`, `Packages.svelte`, `Services.svelte`: per-device hardware and system views
- `components/screens/RemoteAccess.svelte`, `DeviceSettings.svelte`: the SSH terminal plus the confirmed device actions with an audit trail, and device identity/removal
- `DeviceModal.svelte`: device detail; still owns the Files / Packages / Saves / Notes / Docker / Security tabs, and can render inline via `variant="page"` rather than as a popup
- `DockerTab.svelte`: container management (list, start/stop/restart/kill/remove, exec, compose editor, prune)
- `SecurityTab.svelte`: Lynis security audit runner, hardening index gauge, warnings/suggestions, history
- `SystemMetrics.svelte`: detailed hardware telemetry display and history charts
- `NotificationsModal.svelte`: notification provider settings and inbox
- `NotificationToast.svelte`: real-time toast notifications via SSE

**Status:** the shell and every fleet and device screen are implemented. A few screens render honest "not collected yet" states for the parts whose API does not exist: `Services` (systemd units, open ports), the pending-updates half of `Packages`, and the SMART half of `Storage`. `RemoteAccess` hosts the SSH terminal (over the remote-access tunnel) and the confirmed device actions (reboot / update-packages / restart-agent). Those are marked in the UI with the missing endpoint, and `lib/flags.ts` (`VITE_MOCK_*`) can swap in labelled sample data for layout review. `cpu`, `memory`, `network` and `sensors` currently all resolve to the same `DeviceOverview`; `containers` and `security` still resolve to `DeviceModal` tabs. `logs` is a dedicated screen (`DeviceLogs`) that the sidebar section and the modal tab both render.

Note that `svelte-check` does not reliably catch malformed Svelte block structure in this repo; `vite build` is the trustworthy gate for template changes.

## Responsibilities

### Server responsibilities

- device registry and heartbeat tracking
- REST API and persistence (SQLite, WAL mode)
- storage of preference files as small text payloads and base64-encoded saves
- last sync timestamps and online status computation (online <30s, stale >30s, offline >5min)
- web serving and health endpoints
- user authentication (register, login, HttpOnly session cookies, CSRF-protected mutations)
- Docker command queue management and broadcast to agents
- interactive remote-session brokering: one-time tickets and a byte relay between the browser terminal and the agent's outbound tunnel, with the server never naming a host or port
- notification dispatch (Telegram, webhook, web inbox) with 15-minute throttle window
- device notes and file attachments
- structured JSON logging with secret redaction
- telemetry history storage and downsampled aggregation
- enrollment token generation and device enrollment

### Agent responsibilities

- local collection of small preference files and game saves
- sync scheduling and retry logic with exponential backoff and jitter
- safe file selection and validation (allowlist, size limits, secret detection, binary rejection)
- device heartbeat and metadata submission
- hardware telemetry collection (CPU, memory, disk, network, temperatures, battery, processes)
- Docker container monitoring and command execution
- installed application inventory collection
- local policy enforcement
- hardware fingerprint metadata collection without using it as an authenticator

### Web responsibilities

- list devices online/offline with status badges
- show last sync timestamp
- show user metadata and device detail
- display and inspect currently synced preference files
- display live hardware telemetry with periodic refresh (CPU, memory, disk I/O charts)
- show installed packages with search and lazy loading
- show synced save files with sizes and timestamps
- Docker container management (start/stop/restart/kill/remove, exec, compose, prune)
- device notes and attachments
- notification provider configuration and inbox
- real-time notification push via SSE
- the **Remote access** screen: an SSH terminal (xterm.js) plus the device action commands and the recent-session audit list

## Communication

Communication is API-first and intentionally simple:

1. The agent reads small preference files from selected application or user paths.
2. The agent sends the payload to the server together with metadata such as device ID and synced-at timestamp.
3. The server stores small text files and JSON metadata in SQLite and mirrors them to `/data/<device>/<category>/`.
4. The web dashboard reads the server data and shows the current device state.
5. Docker management commands flow from the dashboard to the server (command queue), then to the agent (polling), and results flow back.
6. Notifications flow from the server dispatcher to enabled providers asynchronously.
7. For remote access, the agent keeps an outbound tunnel; the server issues a one-time ticket, and bytes flow browser → server → agent → loopback sshd. The server never names a host or port.

No direct server-to-client shell execution is allowed, with one scoped exception:
the opt-in remote-access tunnel, which is mediated by the device's local policy
and terminates in the device's own sshd (see SECURITY.md).

## Architectural principles

- Agent-first architecture.
- Server is passive and safe.
- No remote arbitrary shell execution, except the opt-in remote-access tunnel, which is gated by the device's local policy and terminates in the device's own sshd.
- Only small text files are saved (up to 256 KiB) and binary saves as base64 (up to 1 MiB).
- Device state is tracked by online status and last synchronization.
- Only explicit preference files are synced (allowlist-based).
- No full-home synchronization by default.
- No broad backup of ~/.config or other large directories.
- User data is treated as explicit and filtered by allowlist.
- Docker management is proxied through the agent command queue with container ID validation.
- The contract (`COLLECTION-CONTRACT.md` + `agent/internal/contract/contract.json`) is the single source of truth.
- Account-first auth: devices are always bound to an owner account.
- Notifications use pluggable providers with encrypted credentials and non-blocking fan-out.
- The agent never writes outside its own state directories and never runs privileged.

## Agent updates

The agent ships fixes to itself without anyone logging into a device, which is
harder than it sounds: the daemon runs as an unprivileged service user, so it can
neither replace `/usr/local/bin/sync-win-agent` nor write `/etc/systemd/system`.

Two root-owned units close that gap, and the update service reconciles **both**
the binary and the unit on every run:

- `sync-win-agent-update.timer` — checks every 15 minutes.
- `sync-win-agent-update.path` — fires the same service when the daemon writes
  `AgentUpdateRequestPath` (`/var/lib/sync-win/update-request`), a file it may
  write because the directory is in the unit's `ReadWritePaths`.

The daemon drives its own upgrade through the path unit, so it no longer depends
on the timer having been installed. It requests an update when the server reports
a newer version, and a **unit repair** when it can read neither the journal nor
its configuration — a unit problem the daemon cannot fix itself.

Reconciling the unit independently of the binary matters: a device can be fully
up to date and still missing a group or a flag that a feature depends on. An
early version of the updater returned early when the binary was current, which
left a fleet running without journal access while looking perfectly healthy.

Credentials for the authenticated unit fetch come from the unit itself
(`--device-id`/`--device-token`), not from the agent state file: the update
service runs as root, where the state path resolves under a different `HOME`.

## Project limits

The current project intentionally does not include:

- restore of full system environments
- snapshots or full machine images
- remote command execution (except Docker management via the proxy, and the
  opt-in remote-access SSH tunnel)
- large binary or media backups
- real-time sync for every file in the home directory

## Technological decisions

- Go for server and agent: small runtime, good tooling, fast binaries.
- SQLite for persistence: low operational overhead (pure-Go driver via modernc.org/sqlite, WAL journal mode, schema applied idempotently by `store.initSchema`, automatic import of legacy `sync-win-store.json` on first start).
- Docker and Docker Compose for the server runtime.
- Svelte 5 + TypeScript 6 + Vite 8 + Chart.js 4 for the dashboard: lightweight UI.
- Small text files and base64-encoded saves as the main persistence model.
- Explicit collection contract (`COLLECTION-CONTRACT.md` + embedded `agent/internal/contract/contract.json`): the agent owns command execution, validates everything locally, and only sends what the contract defines.
- Multi-stage Dockerfile: Go build + Node build -> Alpine runtime.
- Production dashboard served by the Go server from the same origin (no CORS).

## Security rules

- Never execute shell commands on client machines from the server. The one
  exception is the opt-in remote-access tunnel (dual-gated by
  `SYNCWIN_ENABLE_REMOTE_ACCESS` and the device's `allow_remote_access`), where
  the agent — not the server — dials the actual connection.
- Never store secrets, tokens, passwords, or private keys.
- Ignore sensitive data by default.
- Validate and sanitize every uploaded file.
- Keep the data model limited to small, explicit user preferences.
- Never copy the entire home directory.
- Docker exec commands are validated and capped at 30s timeout with 64KB output limit.
- Docker compose file writes are restricted to valid compose filenames only.
- All Docker actions require authenticated device ownership and are logged in the audit trail.
- User passwords are stored as salted Argon2id hashes; session and device credentials are hashed at rest.
- Dashboard mutations use an HttpOnly session cookie plus CSRF validation; owner resources are isolated by session.
- Hardware fingerprints are never accepted as authentication or reconnect credentials.
- Webhook destinations are checked against private-network ranges and redirects are disabled.
- Notification provider secrets are encrypted with AES-GCM before storage.
- HTTP request bodies are capped at 2 MiB.
- Enrollment tokens are single-use with 15-minute expiry.
- Agent binary integrity is verified via SHA-256 checksum at install time.

## Performance

- Keep server idle memory low.
- Prefer explicit allowlists over broad filesystem scans.
- Store small text files instead of large backups.
- Avoid background work that scans the full user home.
- Agent content hashes prevent re-uploading unchanged files.
- Telemetry downsampled aggregation reduces query load.
- Web dashboard uses lazy loading and 60-second cache TTL for large datasets.
- `GET /api/devices` returns a lightweight summary projection (no app inventory,
  no device token, trimmed hardware) and JSON API responses are gzip-compressed.
- Device cards use canvas sparklines; Chart.js is only loaded in the detail and
  history modals.
- Agent samples system logs every 6th telemetry cycle and only rewrites its
  state file when it changes.
- Device log lines are persisted to their own `device_logs` table rather than
  living in the `hardware_json` blob, which is overwritten on every telemetry
  post. Ingestion is idempotent on `(device_id, ts, source, message)`, so the
  overlapping windows an agent re-ships cost one `INSERT OR IGNORE` per line and
  rows are aged out after 7 days.

### Device log storage requires a journal group

The Logs screen depends on the agent being able to read the journal, and that is
a permission the unit has to grant explicitly. Journal files are
`0640 root:systemd-journal`, so an agent running as `sync-win` without
membership of `systemd-journal` (or `adm`) gets `Permission denied` from
`journalctl` and reports nothing.

The failure is silent from the dashboard's point of view: the agent looks
healthy, telemetry keeps flowing, and the screen shows "no logs yet" for every
device at once. It was misread as a server-side storage bug during this work —
the `device_logs` table was empty with online devices reporting, and the
telemetry payload had no `logs` key at all, which pointed at the agent rather
than the server.

So: `SupplementaryGroups=docker systemd-journal` in both
`server/sync-win-agent.service` and the unit `scripts/install.sh` writes, and
`CollectDeviceLogs` reports the `journalctl` stderr instead of just an exit
status. `COLLECTION-CONTRACT.md` records how to check a running agent.

### Device log storage

The Logs screen could not be built on `hardware_json`. That column is replaced
wholesale by every telemetry post, and the agent only samples the journal on one
cycle in six, so a batch was erased by the next cycle that carried no logs — the
screen rendered empty roughly five sixths of the time.

Journal lines therefore get their own table and their own endpoint,
`GET /api/devices/{id}/logs`. The server owns the store and the query surface
(level, source, substring search, time range, paging); the agent owns only the
sampling and the redaction. The `logs[]` field still travels inside the telemetry
payload, because the agent samples on a slow cycle and a dedicated endpoint for a
once-a-minute batch would be a second failure path for no gain.

The viewer is one component (`DeviceLogs.svelte`) used by both the sidebar Logs
section and the device modal tab. When those were separate, they drifted — which is
the failure `web/src/lib/types.ts` documents for duplicated shapes.

## Future evolution

The architecture is intentionally extensible:

- user preference categories
- more device information fields
- richer sync policies
- per-user preference sets
- stronger account and device identity management
- more observability around synchronization health

The initial implementation should remain minimal, safe, and easy to reason about.
