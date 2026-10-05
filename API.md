# REST API Reference

All endpoints return JSON. Requests with bodies must set `Content-Type: application/json`.

## Authentication

Dashboard sessions use the `syncwin_session` HttpOnly cookie. Non-browser clients
may use the compatibility bearer token returned by login/register:

```
Authorization: Bearer <session-token>
```

Cookie-authenticated state-changing requests must also send the double-submit
CSRF header:

```
X-SYNCWIN-CSRF: <value of the syncwin_csrf cookie>
```

Token types:
- **Session token**: from login/register, used by the dashboard/API; only a hash is stored server-side
- **Device token**: returned only during enrollment, used by the agent; only a hash is stored server-side

The legacy `X-SYNCWIN-Anonymous-ID` header and `owner_id` query parameter are not
authentication mechanisms. Every dashboard resource is scoped to the session
owner.

## Base URL

```
http://localhost:8080
```

---

## Health

### `GET /health`

Health check endpoint. No authentication required.

**Response:**
```json
{
  "status": "ok",
  "version": "0.5.8"
}
```

---

## Authentication

### `POST /api/auth/register`

Register a new user account.

**Request:**
```json
{
  "email": "user@example.com",
  "password": "securepassword"
}
```

**Response:** `200 OK` (the server also sets `syncwin_session` and `syncwin_csrf` cookies)
```json
{
  "token": "session-token",
  "owner_id": "user-id",
  "email": "user@example.com"
}
```

### `POST /api/auth/login`

Log in with existing credentials.

**Request:**
```json
{
  "email": "user@example.com",
  "password": "securepassword"
}
```

**Response:** `200 OK` (the server also sets `syncwin_session` and `syncwin_csrf` cookies)
```json
{
  "token": "session-token",
  "owner_id": "user-id",
  "email": "user@example.com"
}
```

### `GET /api/auth/me`

Get the current authenticated user. Requires session token.

Session tokens last 30 days by default. Override the lifetime with the
`SYNCWIN_SESSION_TTL_HOURS` environment variable (e.g. `720` = 30 days). When a
session expires, clients receive `401` and must log in again; the dashboard
shows the login screen and never falls back to an anonymous device list.

**Response:** `200 OK`
```json
{
  "id": "user-id",
  "email": "user@example.com",
  "created_at": "2024-01-01T00:00:00Z"
}
```

### `POST /api/auth/update-email`

Update the current user's email.

**Request:**
```json
{
  "email": "newemail@example.com"
}
```

**Response:** `200 OK`

### `POST /api/auth/update-password`

Change the current user's password.

**Request:**
```json
{
  "current_password": "oldpassword",
  "new_password": "newpassword"
}
```

**Response:** `204 No Content`

All existing sessions are revoked after a password change.

### `POST /api/auth/logout`

Revoke the current session and clear the browser cookies.

**Response:** `204 No Content`

---

## Agent

### `GET /api/agent/version`

Returns the agent version from the embedded contract. No authentication required.

**Response:** `200 OK`
```json
{
  "version": "0.5.8"
}
```

### `POST /api/agent/enroll-token`

Generate a one-time enrollment token. Requires session token.

**Response:** `201 Created`
```json
{
  "token": "abc123...",
  "expires_at": "2024-01-01T00:15:00Z"
}
```

### `POST /api/agent/enroll`

Enroll a new device using an enrollment token. Called by the installer.

**Request:**
```json
{
  "token": "enrollment-token",
  "hostname": "my-linux-machine"
}
```

**Response:** `201 Created`
```json
{
  "device_id": "dev-1",
  "device_token": "device-secret-token"
}
```

### `POST /api/agent/reconnect`

Always returns `410 Gone`. Hardware fingerprints are observable metadata and
are never accepted as proof of identity. If an agent loses its device token,
generate a new enrollment token and enroll again.

### `GET /api/agent/download`

Download the agent binary. No authentication required (protected by enrollment token in the install flow).

### `GET /api/agent/install.sh`

Returns the agent install script with enrollment token embedded. No authentication required.

### `GET /install/{token}`

Serves the install script with the enrollment token embedded in the URL. Used by the one-line installer.

### `GET /api/agent/checksums`

Returns SHA-256 checksums for agent binaries. No authentication required.

---

## Devices

### `GET /api/devices`

List all devices for the authenticated owner as lightweight summaries. The
response intentionally omits the app inventory, the device token, and heavy
hardware fields (logs, top processes, Docker containers, per-core usage). API
JSON responses are gzip-compressed when the client sends `Accept-Encoding: gzip`.

**Response:** `200 OK`
```json
[
  {
    "id": "dev-1",
    "hostname": "my-machine",
    "status": "online",
    "last_seen_at": "2024-01-01T12:00:00Z",
    "last_sync_at": "2024-01-01T11:55:00Z",
    "preference_count": 7,
    "app_count": 150,
    "saves_count": 25,
    "saves_size_bytes": 1048576,
    "hardware": { "cpu_usage_percent": 12.5, "memory_used_bytes": 0, "...": "..." }
  }
]
```

### `GET /api/devices/{id}`

List the synced preference files for a device.

**Response:** `200 OK` (array of file objects)

### `GET /api/devices/{id}/detail`

Get a single device with its full hardware payload (logs, top processes, Docker
state, per-core usage) for the detail modal. The device token is never returned.
The app inventory is fetched separately via `GET /api/devices/{id}/apps`.

**Response:** `200 OK` (device object with full `hardware`)

### `DELETE /api/devices/{id}`

Delete a device and all its associated data. Requires session token.

**Response:** `200 OK`

---

## Device Sub-Resources

All device sub-resource endpoints are under `/api/devices/{id}/...` and require device token authentication (for agent endpoints) or session token (for dashboard endpoints).

### Heartbeat

#### `POST /api/devices/{id}/heartbeat`

Send a heartbeat from the agent. Resets the sync failure counter and marks the device as online.

**Request:**
```json
{
  "device_token": "device-secret-token"
}
```

**Response:** `200 OK`

### Sync

#### `POST /api/devices/{id}/sync`

Sync preference files from the agent to the server.

**Request:**
```json
{
  "device_token": "device-secret-token",
  "preferences": [
    {
      "category": "shell",
      "filename": "bashrc",
      "relative_path": ".bashrc",
      "content": "# bashrc content"
    },
    {
      "category": "saves",
      "filename": "Slot_00000002.save",
      "relative_path": ".config/hydralauncher/wine-prefixes/1222670/drive_c/users/steamuser/Documents/Electronic Arts/The Sims 4/saves/Slot_00000002.save",
      "content": "<base64>",
      "encoding": "base64"
    }
  ]
}
```

**Response:** `200 OK`
```json
{
  "saved": [ ... ],
  "rejected": [
    { "filename": "secrets.env", "reason": "secret content detected" }
  ]
}
```

### Telemetry

#### `POST /api/devices/{id}/telemetry`

Submit hardware telemetry from the agent.

**Request:**
```json
{
  "device_token": "device-secret-token",
  "hardware": {
    "cpu_usage_percent": 45.2,
    "memory_used_bytes": 4294967296,
    "memory_total_bytes": 8589934592,
    "cpu_temperature": 65.0,
    "power_watts": 15.0,
    "battery_percent": 85.0,
    "battery_status": "charging",
    "agent_version": "0.5.8",
    "operating_system": "linux",
    "architecture": "amd64",
    "cpu_model": "Intel Core i7-12700K",
    "kernel_version": "6.1.0",
    "desktop_environment": "kde",
    "locale": "en_US",
    "timezone": "America/Sao_Paulo",
    "uptime_seconds": 86400,
    "boot_time": "2024-01-01T00:00:00Z",
    "load_average": "0.50 0.75 1.00",
    "disk_read_bytes": 1048576,
    "disk_write_bytes": 524288,
    "disk_read_rate": 104857.6,
    "disk_write_rate": 52428.8,
    "docker_available": true,
    "docker_containers": [ ... ],
    "docker_info": { ... }
  }
}
```

**Response:** `200 OK`

#### `GET /api/devices/{id}/telemetry/history`

Get raw telemetry history for charts. Requires session token.

**Query parameters:**
- `period`: time period (`1h`, `6h`, `24h`, `7d`, `30d`)

**Response:** `200 OK` (array of telemetry snapshots)

### Apps

#### `POST /api/devices/{id}/apps`

Submit installed application inventory from the agent.

**Request:**
```json
{
  "device_token": "device-secret-token",
  "apps": [
    { "name": "firefox", "version": "121.0", "source": "apt" },
    { "name": "steam", "version": "1.0.0.78", "source": "flatpak" }
  ]
}
```

**Response:** `200 OK`

### Commands

#### `GET /api/devices/{id}/commands`

Poll for pending commands. Called by the agent.

**Response:** `200 OK` (array of pending commands, or empty array)

#### `POST /api/devices/{id}/commands/{cmd_id}`

Report command result. Called by the agent.

**Request:**
```json
{
  "device_token": "device-secret-token",
  "status": "completed",
  "message": "output or result"
}
```

**Response:** `200 OK`

### Files

#### `GET /api/devices/{id}/files`

List all preference files for a device. Requires session token.

**Response:** `200 OK` (array of preference files)

#### `DELETE /api/devices/{id}/files/{file_id}`

Delete a specific preference file. Requires session token.

**Response:** `200 OK`

### App Install

#### `POST /api/devices/{id}/app-install`

Queue an application installation command.

**Request:**
```json
{
  "source": "apt",
  "name": "htop"
}
```

**Response:** `200 OK`

### Save Restore

#### `POST /api/devices/{id}/restore-saves`

Queue a save-game restore command.

**Response:** `200 OK` (currently not fully implemented)

### Sync Config

#### `GET /api/devices/{id}/sync-config`

Get sync configuration for a device. Used by the agent during sync cycles.

**Response:** `200 OK`
```json
{
  "extra_dirs": ["~/my-saves", "/opt/games/saves"]
}
```

### Notes

#### `GET /api/devices/{id}/notes`

List all notes for a device. Requires session token.

**Response:** `200 OK` (array of notes)

#### `POST /api/devices/{id}/notes`

Create a new note for a device. Requires session token.

**Request:**
```json
{
  "content": "Remember to update the GPU drivers"
}
```

**Response:** `201 Created` (created note object)

#### `GET /api/devices/{id}/notes/{note_id}`

Get a specific note. Requires session token.

**Response:** `200 OK` (note object)

#### `DELETE /api/devices/{id}/notes/{note_id}`

Delete a specific note. Requires session token.

**Response:** `200 OK`

### Attachments

#### `GET /api/devices/{id}/attachments`

List all attachments for a device. Requires session token.

**Response:** `200 OK` (array of attachment metadata, without binary data)

#### `POST /api/devices/{id}/attachments`

Upload a file attachment. Requires session token.

**Request:** `multipart/form-data`
- `file`: the file to upload
- `caption`: optional caption text

**Response:** `201 Created` (attachment metadata)

#### `GET /api/devices/{id}/attachments/{att_id}`

Download a specific attachment. Requires session token.

**Response:** `200 OK` (binary file data with appropriate Content-Type)

#### `DELETE /api/devices/{id}/attachments/{att_id}`

Delete a specific attachment. Requires session token.

**Response:** `200 OK`

### Sync Config (Owner)

#### `GET /api/sync-config`

Get sync configuration for the authenticated owner. Requires session token.

**Response:** `200 OK`
```json
{
  "extra_dirs": ["~/my-saves", "/opt/games/saves"]
}
```

#### `PUT /api/sync-config`

Update sync configuration for the authenticated owner. Requires session token.

**Request:**
```json
{
  "extra_dirs": ["~/my-saves", "/opt/games/saves", "~/backups"]
}
```

**Response:** `200 OK`

---

## Docker Management

Docker endpoints are under `/api/devices/{id}/docker/...` and require device token authentication (for agent polling) or session token (for dashboard commands).

### Agent Endpoints

#### `GET /api/devices/{id}/docker/pending`

Poll for pending Docker commands. Called by the agent.

**Response:** `200 OK` (array of pending Docker requests, or empty array)

#### `POST /api/devices/{id}/docker/result`

Report Docker command result. Called by the agent.

**Request:**
```json
{
  "device_token": "device-secret-token",
  "request_id": "dreq-abc123",
  "status": "completed",
  "message": "{ ... }"
}
```

**Response:** `200 OK`

### Dashboard Endpoints

#### `POST /api/devices/{id}/docker/request`

Queue a Docker command from the dashboard. Requires session token.

**Request:**
```json
{
  "type": "start",
  "target": "container-id-or-name"
}
```

**Response:** `200 OK`
```json
{
  "request_id": "dreq-abc123"
}
```

#### `GET /api/devices/{id}/docker/result/{request_id}`

Get the result of a Docker request. Requires session token.

**Response:** `200 OK` (result object) or `202 Accepted` (still pending)

#### `GET /api/devices/{id}/docker/state`

Get current Docker container state. Requires session token.

**Response:** `200 OK` (container list with state)

#### `GET /api/devices/{id}/docker/stream`

SSE stream for real-time Docker state updates. Requires session token.

### Docker Sub-Paths

#### `POST /api/devices/{id}/docker/action`

Perform a container action (start, stop, restart, kill, remove).

**Request:**
```json
{
  "action": "stop",
  "container_id": "abc123def456"
}
```

**Response:** `200 OK`

#### `POST /api/devices/{id}/docker/logs`

Get container logs.

**Request:**
```json
{
  "container_id": "abc123def456",
  "tail": 200
}
```

**Response:** `200 OK` (log output)

#### `POST /api/devices/{id}/docker/exec`

Execute a command inside a container.

**Request:**
```json
{
  "container_id": "abc123def456",
  "command": ["ls", "-la", "/app"]
}
```

**Response:** `200 OK` (command output)

#### `POST /api/devices/{id}/docker/compose`

Perform a compose operation.

**Request:**
```json
{
  "action": "up",
  "path": "/home/user/docker-compose.yml"
}
```

**Actions:** `read`, `write`, `up`, `down`, `ps`, `logs`

**Response:** `200 OK`

#### `POST /api/devices/{id}/docker/prune`

Prune Docker resources.

**Request:**
```json
{
  "target": "system"
}
```

**Targets:** `system`, `image`, `container`, `network`

**Response:** `200 OK`

---

## Security Audit

Security audit endpoints are under `/api/devices/{id}/security/...` and require session token authentication.

#### `POST /api/devices/{id}/security/audit`

Queue a Lynis security audit for the device. Requires session token.

**Response:** `200 OK` (command object)

**Note:** Lynis must be installed on the device (`sudo apt install lynis`). If Lynis is not installed, the agent returns a failed command with instructions.

#### `GET /api/devices/{id}/security/audits`

List audit history for the device. Requires session token.

**Query params:** `limit` (default 20, max 100)

**Response:** `200 OK`
```json
[
  {
    "id": "uuid",
    "device_id": "uuid",
    "hardening_index": 67,
    "total_warnings": 12,
    "total_suggestions": 23,
    "total_tests": 180,
    "tests_passed": 155,
    "lynis_version": "3.0.8",
    "os_info": "Ubuntu 22.04 LTS",
    "kernel_version": "5.15.0-91-generic",
    "report_json": "{...}",
    "created_at": "2026-01-01T12:00:00Z"
  }
]
```

#### `GET /api/devices/{id}/security/audits/{audit_id}`

Get a single audit with full report. Requires session token.

**Response:** `200 OK` (full audit object including `report_json`)

#### `DELETE /api/devices/{id}/security/audits/{audit_id}`

Delete an audit record. Requires session token.

**Response:** `204 No Content`

---

## Sync Config

### `GET /api/sync-config`

Get extra save-game directories for the authenticated owner. Requires session token.

**Response:** `200 OK`
```json
{
  "extra_dirs": ["~/my-saves", "/opt/games/saves"]
}
```

### `PUT /api/sync-config`

Update extra save-game directories. Requires session token.

**Request:**
```json
{
  "extra_dirs": ["~/my-saves", "/opt/games/saves"]
}
```

**Response:** `200 OK`

---

## Logging and Observability

### `GET /api/logs`

Query server logs. Requires session token.

**Query parameters:**
- `level`: filter by log level (info, warn, error)
- `device_id`: filter by device
- `event`: filter by event type
- `limit`: max results (default 100)
- `offset`: pagination offset

**Response:** `200 OK` (array of log entries)

### `GET /api/logs/{id}`

Get a specific log entry. Requires session token.

**Response:** `200 OK` (log entry object)

### `GET /api/logs/stats`

Get log statistics. Requires session token.

**Response:** `200 OK` (aggregated stats)

### `GET /api/audit`

Get audit trail (Docker commands, auth events). Requires session token.

**Response:** `200 OK` (array of audit entries)

### `GET /api/errors`

Get error log entries. Requires session token.

**Response:** `200 OK` (array of error entries)

### `DELETE /api/logs/delete`

Delete logs older than retention period. Requires session token.

**Response:** `200 OK`

### `GET /api/retention`

Get telemetry retention settings. Requires session token.

**Response:** `200 OK`
```json
{
  "raw_hours": 2,
  "resolution_1m_days": 7,
  "resolution_5m_days": 30,
  "resolution_1h_days": 365
}
```

### `PUT /api/retention`

Update telemetry retention settings. Requires session token.

**Request:**
```json
{
  "raw_hours": 4,
  "resolution_1m_days": 14,
  "resolution_5m_days": 60,
  "resolution_1h_days": 365
}
```

**Response:** `200 OK`

---

## Notifications

### `GET /api/notifications/config`

Get notification configurations for the authenticated owner. Requires session token.

**Response:** `200 OK` (array of notification configs)

### `PUT /api/notifications/config`

Update notification configuration. Requires session token.

**Request:**
```json
{
  "provider": "telegram",
  "enabled": true,
  "events": ["device_offline", "device_online"],
  "config": {
    "bot_token": "...",
    "chat_id": "..."
  }
}
```

**Response:** `200 OK`

### `GET /api/notifications/providers`

List available notification providers. Requires session token.

**Response:** `200 OK`
```json
[
  {
    "name": "web_inbox",
    "label": "Web Inbox",
    "description": "In-app notification inbox",
    "public_fields": []
  },
  {
    "name": "telegram",
    "label": "Telegram",
    "description": "Telegram bot notifications",
    "public_fields": ["chat_id"]
  },
  {
    "name": "webhook",
    "label": "Webhook",
    "description": "HTTP webhook notifications",
    "public_fields": ["url"]
  }
]
```

### `POST /api/notifications/test`

Test a notification provider. Requires session token.

**Request:**
```json
{
  "provider": "telegram",
  "config": {
    "bot_token": "...",
    "chat_id": "..."
  }
}
```

**Response:** `200 OK`

### `GET /api/notifications/telegram/detect-chat`

Detect Telegram chat ID from bot token. Requires session token.

**Query parameters:**
- `bot_token`: Telegram bot token

**Response:** `200 OK`
```json
{
  "chat_id": "123456789",
  "chat_title": "My Chat"
}
```

### `GET /api/notifications/inbox`

Get notification inbox events. Requires session token.

**Query parameters:**
- `since`: return events with id > since (for polling)

**Response:** `200 OK` (array of notification events)

### `POST /api/notifications/inbox/read`

Mark notification events as read. Requires session token.

**Request:**
```json
{
  "ids": [1, 2, 3]
}
```

**Response:** `200 OK`

### `POST /api/notifications/stream-token`

Issue a single-use ticket for the SSE connection. Requires a valid session and,
for cookie authentication, `X-SYNCWIN-CSRF`.

**Response:** `200 OK`
```json
{"ticket":"short-lived-ticket"}
```

### `GET /api/notifications/stream`

SSE stream for real-time notification push. Requires the session cookie and a
single-use `ticket` query parameter. The ticket is bound to the session owner
and expires after 30 seconds; it cannot be replayed.

---

## Static Files

### `GET /`

Serves the production web dashboard from `SYNCWIN_WEB_DIR` (default `/app/web`). Falls back to `index.html` for SPA routing.

---

## Error Responses

All error responses follow this format:

```json
{
  "error": "human-readable error message"
}
```

HTTP status codes:
- `200`: success
- `201`: created
- `202`: accepted (still processing)
- `400`: bad request
- `401`: unauthorized (missing or invalid session/device token)
- `403`: forbidden (CSRF or owner mismatch)
- `404`: not found
- `413`: request too large (body > 2 MiB)
- `429`: rate limit exceeded
- `500`: internal server error
