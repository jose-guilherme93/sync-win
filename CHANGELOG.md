# Changelog

All notable, user-facing changes to SyncWin are documented here. The Release
workflow reads the section matching the version it is about to publish and uses
it as the GitHub Release body. A missing or empty section stops the release
before the image is built, so a tag always carries written notes.

The next version is the last tag plus the Conventional Commits bump:
`feat:` increases the minor version, everything else increases the patch
version, and a breaking change increases the major version.

## [Unreleased]

## [0.12.0] - 2026-10-18

### Changed

- **Docker screen rebuilt** to match the rest of the dashboard: tiles for
  running, stopped, paused and images, and a searchable container list with
  state filters. Clicking a container expands a full-width panel below it with
  live CPU, memory, network and disk metrics, configuration detail, logs and a
  terminal. Compose and cleanup are top-level tabs instead of hidden sections.
- **The container list is live.** The screen subscribes to the server's Docker
  state stream, and Refresh asks the device for a fresh list instead of
  re-reading a snapshot that can be a minute old.
- **Container actions answer in about two seconds.** The agent drains the Docker
  request queue on its own two-second poller rather than waiting for the
  telemetry cycle, and a failed command states the device's reason.
- **Pause/unpause and kill** join the existing start/stop/restart/remove actions.
  Paused containers now show a Resume action instead of an unusable Start.
- **Container configuration** in the expanded panel: networks and addresses,
  healthcheck state, restart policy, entrypoint/command, mounts, labels, CPU
  and memory limits. Environment variables are never sent, and
  credential-looking command arguments and labels are masked.
- Paused containers are shown as needing attention on the device instead of
  offering start/restart buttons that Docker would reject.

### Fixed

- Published container ports now appear. The agent decoded the Engine's
  `PrivatePort`/`PublicPort` keys into underscore-tagged fields, so every port
  arrived as zero and the screen showed "no ports".
- A compose scan now reports "no compose project found" when the device simply
  has none, instead of an error caused by a `null` reply or a duplicate empty
  state next to a real error.
- Disabled Docker mutations now answer 403 with "Docker mutations are disabled"
  instead of a masked 503 "internal server error".
- A completed container list now refreshes the cached snapshot, so the screen
  that requested it, the next screen that opens, and every live subscriber see
  the same state after an action.

## [0.11.0] - 2026-10-18

### Added

- **Notification center** under Administration: a full inbox with event and
  device filters, unread-only, mark one or all as read, and a link to the
  device. The bell now shows a preview and opens the center; the old settings
  modal is gone.
- **Per-event in-app preferences.** The in-app channel can be enabled or
  disabled and each event type can be included or excluded from popups. Events
  excluded from popups still appear in the inbox.
- **Sound preferences.** Beep, chime, pulse or silent, with volume and a preview
  button. The choice is stored on the server with the account, so it follows the
  browser and applies to the next notification immediately.
- **Server event catalog** (`GET /api/notifications/events`) with the label,
  description and severity of every event, shared by the in-app, Telegram and
  webhook preferences.
- Mark-all-read uses a server-side flag, so it also covers notifications older
  than the page the browser has loaded.

### Fixed

- Disabling in-app alerts and changing the sound previously had no effect: the
  dispatcher recorded and broadcast every event before provider filtering, and
  the toast read a localStorage value that the settings modal never updated.
- The first notification could be silent because every toast created a new
  `AudioContext` before a user gesture; the app now uses one shared context,
  unlocked on the first interaction.

## [0.10.2] - 2026-10-18

Release-process release. The runtime is the same as 0.10.1.

### Changed

- Every release now has written notes in `CHANGELOG.md`. The release workflow
  publishes that section as the GitHub Release body and refuses to publish an
  image without it.
- Backfilled the notes for 0.10.0 and 0.10.1, which previously contained only a
  Full Changelog link.

### Highlights shipped in 0.10.x

- **Live device screens.** Status and telemetry now follow the device poll on
  every screen. The heartbeat poll is a fixed 10s and no longer depends on the
  chart window, so a 7-day view cannot make a healthy device look stale. Manual
  refresh updates the heavy detail too, and switching devices resets the live
  series.
- **Application logs in Settings.** The log viewer is filled in (it was empty
  because application logs carry no owner), receives new entries over SSE
  instead of polling, and can trace a request by `request_id` or
  `correlation_id`.
- **Log settings at runtime.** Persistence level, console level and retention
  can be changed without a restart and survive a restart.
- **Telemetry retention in Settings.** Raw, 1-minute, 5-minute and 1-hour
  windows are editable on the same screen; the API now uses the documented
  `resolution_*` field names.
- **Persistent alert acknowledgements.** An acknowledgement survives a reload
  and follows the account across browsers. The identity is the rule, not the
  reading, so "CPU 92%" stays acknowledged when the next sample reads 95%.
- **CPU clock and core voltage** on the CPU & Thermal screen when the host
  exposes them.
- **Agent housekeeping.** Lynis availability is logged once per change instead
  of every cycle, routine telemetry is logged once a minute, installer backups
  and LEM-era units/binaries/directories are removed automatically, and a stable
  fingerprint lets the server adopt a machine-less device record instead of
  creating a duplicate.
- **Lower telemetry cost.** The Docker container list is sampled once a minute
  and stored once per device in its own column. A normal sample drops from about
  13 KB to about 3 KB.

## [0.10.1] - 2026-10-18

Patch release. Supersedes 0.10.0 and is the recommended build.

### Fixed

- The agent updater's cleanup commands are bounded by a 10-second context. This
  was the lint failure in the 0.10.0 CI run; it has no user-facing behaviour
  change.

### Included from 0.10.0

- Application logs in Administration → Settings, with filters, request tracing
  and a live SSE tail.
- Runtime log persistence level, console level and retention.
- Telemetry retention in Settings.
- Persistent alert acknowledgements.
- CPU clock and core voltage.
- Live status and telemetry on every device screen.
- Agent log-noise reduction, automatic cleanup of old install remnants, and
  fingerprint adoption.
- Docker container sampling that cuts normal telemetry from about 13 KB to
  about 3 KB.

## [0.10.0] - 2026-10-18

### Added

- **Application logs screen** under Administration → Settings: level, category,
  device, time-window and search filters; request and correlation identifiers;
  and a live Server-Sent Events tail instead of a five-second page reload.
- **Global log settings**: persistence level, console level and retention days,
  applied immediately and persisted across restarts.
- **Telemetry retention in Settings**: raw hours, 1-minute, 5-minute and 1-hour
  retention windows for the account's devices, with the server's validation
  message shown when a value is rejected.
- **Persistent alert acknowledgements** per account, with restore; the alert id
  is the rule (`cpu`, `disk:/`, `net:eth0`, …) rather than the rendered value.
- **CPU clock and core voltage** on the CPU & Thermal screen when cpufreq or a
  labelled hwmon CPU rail is available.
- **Live device screens**: the polled summary is merged into the detail payload,
  so status and telemetry stay current on every screen; the manual refresh also
  refreshes the heavy detail.
- **Agent fingerprint command** and installer integration; the server adopts a
  machine-less device record when the fingerprint matches.
- **Automatic agent housekeeping**: routine journal noise reduced from about 18
  lines per minute to one, installer `.bak`/`.rollback` files removed, and
  LEM-era units, binaries, directories and user cleaned up (the old policy is
  migrated first).
- **Docker container sampling** once a minute, stored in a dedicated column so
  telemetry rows no longer carry the container list.

### Fixed

- The log viewer was empty for every account because application logs carry no
  owner; it is now the fleet-wide operator view, while per-device journal logs
  remain scoped to the device owner.
- The retention API returned `1m_days`/`5m_days`/`1h_days` while the UI and
  documentation used `resolution_*`; saves silently failed validation.
- The log SSE stream emitted the level as a number instead of a string.
- The selected device rendered a frozen detail snapshot, which made the topbar
  report `stale` while Home reported `online`; the chart range also controlled
  the heartbeat poll.
- Switching devices on CPU, Memory, Storage, Network or Sensors mixed the two
  devices' sparkline series.
