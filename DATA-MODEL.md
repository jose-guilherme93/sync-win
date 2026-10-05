# Data Model

This document describes the conceptual data model for the current phase of SyncWin. The product is focused on syncing small user preference files and game save data from Linux devices to the Docker server and showing online status, last synchronization timestamps, live hardware telemetry, and installed packages in the web dashboard.

## User

Represents an identity or owner associated with one or more devices.

Fields:

- id: stable user identifier
- email: contact address (unique, used for login)
- password_hash: salted Argon2id digest (format `argon2id$v=19$...`; legacy `s256$` values are upgraded after login)
- created_at: creation timestamp

Rules:

- passwords are stored as salted Argon2id digests, never in plaintext
- dashboard sessions use an HttpOnly cookie; only a hash of the session token is persisted
- cookie-authenticated mutations require a CSRF token
- each device is bound to an owner account via `owner_id`

## Session

Represents an active login session for a user.

Fields:

- token: SHA-256 hash of the random session token (raw token is returned only at creation)
- owner_id: owning user (foreign key to users)
- created_at: creation timestamp

Rules:

- tokens are stored as hashes server-side with a 30-day TTL
- logout revokes one token; password changes revoke all tokens for the owner
- expired sessions are cleaned up periodically
- the `/api/auth/me` endpoint validates the current session

## Device

Represents a Linux machine running the agent.

Fields:

- id: device identifier
- user_id: legacy user field (may be empty)
- owner_id: owning user (foreign key to users)
- hostname: machine name
- device_token: SHA-256 hash of the agent credential (raw token is returned only during enrollment)
- last_seen_at: last heartbeat timestamp
- last_sync_at: last successful sync
- sync_failures: consecutive failed sync attempts; reset on heartbeat or success
- last_error: last error message
- last_error_at: timestamp of last error
- hardware_json: JSON blob with latest hardware telemetry (HardwareStats)
- apps_json: JSON array with latest installed application inventory
- status: derived from last_seen_at (online <30s, stale >30s, offline >5min, error, duplicate)
- hardware_fingerprint: internal hardware metadata only; never an authentication credential or reconnect secret
- created_at: creation timestamp
- updated_at: modification timestamp

Rules:

- device identity should remain stable across reboots
- device tokens are never shown in the UI or written to server logs
- fingerprint-based reconnect is disabled; a lost token requires a new enrollment token
- the dashboard should prioritize online state and last sync time
- agents send heartbeats via `POST /api/devices/{id}/heartbeat`
- a new enrollment always creates a new device credential
- fingerprint metadata must not be returned by dashboard APIs

## PreferenceFile

Represents a small text file or binary save-game file synced from a device.

Fields:

- id: file record identifier
- device_id: owning device (foreign key to devices)
- user_id: owning user
- category: general, kde, desktop, shell, app, saves
- filename: original file name
- relative_path: path relative to the allowlist
- content: text payload, small JSON content, or base64-encoded binary save data
- encoding: "" (utf8) or "base64" for binary save games
- content_hash: SHA-256 hash of the stored content
- size_bytes: size of the file payload (decoded size for base64)
- synced_at: last sync time
- status: synced, pending, invalid, deleted

Rules:

- text files must be small (server limit: 256 KiB per file)
- save-game files (category "saves", encoding "base64") are allowed up to 1 MiB each
- broad directory copying is forbidden
- only explicitly allowed paths should be synced (agent defaults plus `~/.config/sync-win/allowed-files`)
- sensitive files must be excluded by default
- the server rejects binary content for non-saves categories, credential-like filenames, and content matching known secret patterns; rejected items are reported back to the agent with a reason
- for base64-encoded saves, secret scanning is skipped (content cannot be inspected)
- a unique index on `(device_id, category, relative_path, filename)` prevents duplicate files
- content hashes enable skip-unchanged optimization on the agent side

## PreferenceSet

Represents a grouped set of preference files for a device or user.

Fields:

- id: set identifier
- device_id: owning device
- name: preference set name
- updated_at: last update timestamp
- files: list of associated preference files

Rules:

- keep grouping simple and explicit
- works well for per-device UI views

## SyncState

Represents the synchronization state of a device.

Fields:

- id: sync state identifier
- device_id: owning device
- status: idle, syncing, success, failed
- last_sync_at: last sync timestamp
- last_error: optional error message
- retry_count: number of failed sync attempts
- updated_at: timestamp

Rules:

- used by the UI and diagnostics
- should remain simple and lightweight
- in the current implementation, sync state lives directly on the Device record (status, last_sync_at, last_error, sync_failures) instead of a separate entity

## SyncConfig

Represents the operator-configured extra save-game folders for a user's devices.

Fields:

- owner_id: owning user (primary key)
- extra_dirs_json: JSON array of extra directory patterns (e.g., `["~/my-saves", "/opt/games/saves"]`)
- updated_at: last modification timestamp

Rules:

- max 20 extra directories per owner
- each path max 300 characters
- paths containing ".." are rejected
- agents fetch this config via `GET /api/devices/{id}/sync-config` during their preference sync cycle
- built-in Hydra Launcher defaults are embedded in the agent contract and always apply

## EnrollmentToken

Represents a short-lived token for agent installation and device enrollment.

Fields:

- id: token record identifier
- owner_id: owning user (foreign key to users)
- token: 32-character random token (unique)
- created_at: creation timestamp
- expires_at: expiry timestamp (15 minutes after creation)
- used_at: timestamp when token was consumed (empty if unused)
- device_id: device ID created during enrollment (empty if unused)

Rules:

- tokens are single-use: once `used_at` is set, the token cannot be reused
- tokens expire after 15 minutes
- the `/api/agent/install.sh` endpoint serves the install script with the token embedded
- `POST /api/agent/enroll` consumes the token and creates the device
- tokens are bound to the owner who generated them

## DeviceNote

Represents a free-text note attached to a device.

Fields:

- id: note record identifier
- device_id: owning device (foreign key to devices)
- owner_id: owning user
- content: note text content
- created_at: creation timestamp
- updated_at: last modification timestamp

Rules:

- notes are owned by the same account that owns the device
- notes are used for operator annotations and reminders
- the UI displays notes in the device detail modal (Notes tab)

## DeviceAttachment

Represents a file attachment attached to a device.

Fields:

- id: attachment record identifier
- device_id: owning device (foreign key to devices)
- owner_id: owning user
- filename: original filename
- mime_type: MIME type (default: application/octet-stream)
- size_bytes: file size in bytes
- data: binary blob (BLOB)
- caption: optional caption text (added in migration 0010)
- created_at: creation timestamp

Rules:

- attachments are stored as binary blobs in SQLite
- attachments are owned by the same account that owns the device
- the UI displays attachments in the device detail modal (Notes tab)

## ServerLog

Represents a structured log entry for observability.

Fields:

- id: auto-increment identifier
- ts: timestamp
- level: log level (info, warn, error)
- category: log category (device, http, app, etc.)
- event: event type identifier
- message: human-readable summary
- device_id: associated device (if applicable)
- user_id: associated user (if applicable)
- request_id: HTTP request correlation ID
- correlation_id: cross-service operation trace
- duration_ms: request duration in milliseconds
- status: HTTP status code (if applicable)
- metadata: JSON blob with structured context
- redacted: whether secrets were redacted in this entry

Rules:

- logs are append-only and never modified
- secrets are automatically redacted before storage
- deduplication prevents repeated identical entries
- the structured JSON logger writes to both the SQLite table and `data/sync-win-server.log`
- the HTTP access table aggregates request metrics for dashboard queries

## HttpAccess

Aggregated HTTP access metrics for dashboard display.

Fields:

- id: auto-increment identifier
- method: HTTP method
- path: request path
- status_class: status code class (2xx, 3xx, 4xx, 5xx)
- count: request count in the window
- avg_duration_ms: average request duration
- max_duration_ms: maximum request duration
- window_start: aggregation window start
- window_end: aggregation window end

Rules:

- metrics are periodically flushed from in-memory counters
- used for dashboard request performance visualization

## ApplicationMetric

Numeric time-series metrics for application monitoring.

Fields:

- id: auto-increment identifier
- name: metric name
- value: numeric value
- ts: timestamp
- labels: JSON blob with key-value labels

Rules:

- separate from log data
- used for internal application performance monitoring

## TelemetryHistory

Raw time-series telemetry snapshots for real-time charts.

Fields:

- id: auto-increment identifier
- device_id: owning device (foreign key to devices)
- timestamp: snapshot timestamp
- payload: JSON blob with full HardwareStats payload

Rules:

- stores raw telemetry data points
- used by the dashboard for real-time chart rendering
- raw data is retained according to retention settings (default: 2 hours)

## TelemetryDownsampled

Downsampled telemetry for long-term history display.

Fields:

- id: auto-increment identifier
- device_id: owning device (foreign key to devices)
- timestamp: aggregation window timestamp
- resolution: aggregation resolution (`1m`, `5m`, `1h`)
- cpu_avg, cpu_min, cpu_max: CPU usage statistics
- mem_avg, mem_min, mem_max: memory usage statistics
- net_rx_avg, net_tx_avg: network throughput averages
- temp_avg, temp_max: temperature statistics
- power_avg: power consumption average
- sample_count: number of raw samples aggregated

Rules:

- data is automatically aggregated from raw telemetry_history
- `1m` resolution: retained for 7 days
- `5m` resolution: retained for 30 days
- `1h` resolution: retained for 365 days
- retention is configurable per-owner via retention_settings

## RetentionSettings

Per-owner configuration for telemetry data retention.

Fields:

- owner_id: owning user (primary key)
- raw_hours: hours to retain raw telemetry (default: 2)
- resolution_1m_days: days to retain 1m downsampled data (default: 7)
- resolution_5m_days: days to retain 5m downsampled data (default: 30)
- resolution_1h_days: days to retain 1h downsampled data (default: 365)
- updated_at: last modification timestamp

## Request limits

- HTTP request bodies are capped at 2 MiB by the server; larger payloads are rejected with 413
- session tokens are random, stored as hashes server-side with a 30 day TTL, and sent as an HttpOnly cookie (Bearer remains supported for API clients)
- user passwords are stored as salted Argon2id digests

## Storage

- all state lives in SQLite at `/data/sync-win.db` (WAL journal mode, foreign keys on, 5 s busy timeout)
- the schema is defined by `store.initSchema` in the server binary and applied idempotently on startup (there is no separate migrations runner)
- deployments upgrading from the JSON store are migrated automatically: `sync-win-store.json` is imported once and renamed to `sync-win-store.json.migrated`
- preference file contents are mirrored to `/data/<device>/<category>/<filename>` for easy inspection; SQLite remains the source of truth

## SystemInfo

Represents minimal host metadata useful for the dashboard.

Fields:

- id: system record identifier
- device_id: owning device
- hostname: system hostname
- os_name: distro or OS family
- os_version: distro version
- kernel_release: kernel string
- architecture: x86_64 or arm64
- desktop_environment: KDE, GNOME, etc.
- locale: user locale
- timezone: timezone identifier
- agent_version: installed agent version
- captured_at: timestamp

Rules:

- this is not a full environment snapshot
- only metadata needed for visibility and diagnostics is stored
- in the current implementation, system metadata (architecture, desktop environment, locale, timezone, agent version) travels inside the device hardware stats payload

## Capability

Represents a supported feature or preference category.

Fields:

- id: capability identifier
- name: kde_preferences, shell_preferences, app_settings
- description: human-readable description
- enabled: boolean

Rules:

- capabilities are mainly used for feature detection and UI display
- they do not represent restore operations

## Activity / Event

Represents operational events and diagnostics.

Fields:

- id: event identifier
- device_id: owning device
- type: heartbeat, sync_started, sync_success, sync_failed, device_online
- message: plain text summary
- details: structured payload
- created_at: timestamp

Rules:

- event logs are used for troubleshooting and observability
- secrets are never stored in event details

## Notification configuration (`notification_configs`)

Columns:

- id: integer primary key
- owner_id: user who owns this config
- provider: string (`web_inbox`, `telegram`, `webhook`, ...)
- enabled: boolean
- events: JSON array of subscribed event types
- config_enc: AES-GCM encrypted JSON blob (provider-specific fields), base64-encoded
- created_at, updated_at: timestamps

Rules:

- Provider config values containing secrets are encrypted with AES-GCM before storage.
- The frontend never receives raw secret values; `PublicFields()` controls what is safe to expose.
- A unique constraint on `(owner_id, provider)` prevents duplicate configs per provider.
- Supported event types: `device_offline`, `device_online`, `sync_error`, `device_enrolled`.

## Notification events (`notification_events`)

Columns:

- id: integer primary key (monotonically increasing, used as `since` cursor)
- owner_id: user who owns this event
- type: event type (`device_offline`, `device_online`, `sync_error`, `device_enrolled`)
- device_id, hostname: identifying the device
- message: human-readable summary
- read: boolean (false until acknowledged by the user)
- created_at: timestamp

Rules:

- Events are written by the dispatcher when a transition is detected or an error occurs.
- A 15-minute throttle window per `(event_type, device_id)` prevents notification storms.
- Old events are pruned by a background job after `DataRetention.NotificationsHours`.
- The `/api/notifications/inbox` endpoint accepts an optional `since` query parameter to return only events with `id > since`.
- Real-time push to browsers is handled via SSE (`/api/notifications/stream`).

## DeviceCommand

Represents a command queued for a device agent to execute.

Fields:

- id: command record identifier
- device_id: target device (foreign key to devices)
- type: command type (exclude_file, install_app, restore_saves, docker_*)
- path: file path or container ID (context-dependent)
- name: container ID or compose file path (for Docker commands)
- source: source data (e.g., exec command JSON, compose file content)
- payload: additional JSON payload (Docker command results, exec data)
- status: queued, completed, failed
- message: result message or output (may contain JSON for Docker data queries)
- created_at: queue timestamp
- completed_at: completion timestamp

Rules:

- commands are processed by the agent in order of creation
- Docker commands use the name/source/payload fields for container IDs, exec commands, and compose file content
- the message field carries structured JSON results for data queries (docker_list_containers, docker_compose_read)
- Docker exec commands have a 30s timeout and 64KB output cap
- the payload column was added in migration 0012 for Docker command results and exec data

## DockerSummary

Represents a lightweight Docker container snapshot for telemetry.

Fields:

- id: container ID (short, 12 chars)
- name: container name
- image: container image
- state: running, exited, paused, created
- status: human-readable status text

## DockerInfoSummary

Represents Docker engine info for telemetry.

Fields:

- server_version: Docker Engine version
- containers_total: total container count
- containers_running: running container count
- containers_stopped: stopped container count
- images_count: image count
- driver: storage driver (e.g., overlay2)
- ncpu: number of CPUs

Rules:

- Docker info is included in the hardware telemetry payload when Docker is available
- Docker container summaries appear in the device's hardware stats for dashboard overview

## Conceptual relationships

- A User owns multiple Devices and has many Sessions.
- A Device has a current SyncState and may have many PreferenceFile records.
- A Device may have many DeviceNotes and DeviceAttachments.
- A PreferenceSet groups files associated with a device.
- Activity records are generated by both the server and the agent.
- SystemInfo is a lightweight overview of the host environment.
- A User has many NotificationConfigs (one per provider) and NotificationEvents (the inbox).
- EnrollmentTokens are bound to a User and consumed during device enrollment.
- TelemetryHistory and TelemetryDownsampled store time-series data per Device.
- RetentionSettings controls data lifecycle per User.
- ServerLogs, HttpAccess, and ApplicationMetrics provide observability.

## Design goal

The data model favors low-volume, explicit, and safe user preference syncing over large environment capture. The server stores small text-based files, base64-encoded saves, and metadata that help the web UI show which devices are online, when they last synchronized, and what hardware state they are in.
