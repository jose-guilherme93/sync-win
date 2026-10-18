# Changelog

All notable, user-facing changes to SyncWin are documented here. The Release
workflow reads the section matching the version it is about to publish and uses
it as the GitHub Release body. A missing or empty section stops the release
before the image is built, so a tag always carries written notes.

The next version is the last tag plus the Conventional Commits bump:
`feat:` increases the minor version, everything else increases the patch
version, and a breaking change increases the major version.

## [Unreleased]

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
