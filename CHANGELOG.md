# Changelog

All notable, user-facing changes to SyncWin are documented here. The Release
workflow reads the section matching the version it is about to publish and uses
it as the GitHub Release body. A missing or empty section stops the release
before the image is built, so a tag always carries written notes.

The next version is the last tag plus the Conventional Commits bump:
`feat:` increases the minor version, everything else increases the patch
version, and a breaking change increases the major version.

## [Unreleased]

## [0.15.2] - 2026-10-30

### Fixed

- **Release images sign the agent again.** The Docker build layer for the agent
  is cached by BuildKit without the signing secret's content, so the images
  from 0.13.x through 0.15.1 shipped an empty `sync-win-agent.sig`. Every
  updater failed closed at the signature endpoint. The release build now
  invalidates that layer with the commit SHA, requires `AGENT_UPDATE_KEY`, and
  fails instead of publishing an unsigned image.
- **The agent is now 0.11.1.** A device installed from an unsigned image has no
  public key to verify an update, so it needs one manual binary replacement
  (`/api/agent/download` → `/usr/local/bin/sync-win-agent`, then restart the
  service). After that, auto-update works normally. A fresh install from 0.15.2
  is signed and updates without intervention.

## [0.15.1] - 2026-10-30

### Fixed

- **The topology agent now reaches devices that were already enrolled.** The
  0.15.0 image contained the new Tailscale collector but still advertised agent
  `0.10.0`, so the updater compared `0.10.0` with `0.10.0` and never downloaded
  it. The agent is now `0.11.0` and the collection contract `1.9.0`; a device on
  `0.10.0` updates on its next check and can read the tailnet. A fresh install
  from the 0.15.0 image was unaffected.

## [0.15.0] - 2026-10-30

### Added

- **A network map for every device.** The new **Topology** screen draws the
  networks a device belongs to — each local segment and each overlay — with the
  nodes the device can see. The device sits in the middle, every network hangs
  off it as a hub, and the members sit around their hub.
- **Tailscale is part of the map.** The agent reads the tailnet from the
  daemon's local API socket (world readable on a default install, no root and no
  `tailscale` binary), so the map shows the tailnet name, every peer, its OS, its
  overlay address, whether the path is direct or relayed, tags, exit-node status
  and when it was last seen. Offline peers stay on the map, greyed out.
- **The local segment comes from the kernel.** ARP and NDP neighbours are grouped
  by interface, deduplicated, drawn with their MAC vendor, and the gateway is
  shown once as the way out to the internet. Another SyncWin device on the same
  segment or in the same tailnet is recognised and can be opened from the map.
- **The map is interactive.** Drag a node to pin it, scroll to zoom, drag the
  background to pan, reset the view, and click any node or network for a detail
  drawer with addresses, state, path, tags and members.
- **Honest about what it cannot see.** A LAN member is only a device this host
  has exchanged traffic with recently, never the result of a scan; WireGuard and
  other root-owned tunnels are named instead of hidden; an unreadable Tailscale
  socket is reported as an error instead of an empty peer list.
- The Network screen links to the map, and the map refreshes whenever it opens
  and once a minute while it stays open.

## [0.14.1] - 2026-10-28

### Fixed

- **Telemetry charts plot time, not samples.** A window with only a few
  samples no longer stretches them across the whole card. Every point sits at
  its real timestamp inside the selected window, so a fresh chart grows from
  the left as data arrives, gaps stay where they happened and a device that
  reported for ten minutes of an hour shows ten minutes of line, not an hour
  of invented slope. The spline overshoot that drew a peak at the start of a
  series is gone as well.
- **Every plot shares one look.** The Overview, the Network traffic chart and
  the detailed System tab now draw filled areas with Beszel's chart palette
  (blue CPU, green memory, orange temperature and disk writes, pink upload),
  round time ticks, horizontal-only grid lines and one dark tooltip.

## [0.14.0] - 2026-10-22

### Added

- **The Network screen answers the three questions an operator actually asks.**
  It now shows the traffic of each link over one hour, 24 hours or seven days
  (read from the server, not from a ten-minute memory buffer), the health of
  the stack, the IP configuration, the neighbours on the segment and what is
  reachable from outside.
- **Per-interface traffic history.** The server keeps one sample per interface
  per minute, rolled up into five minute buckets after two days. The global
  telemetry history only has the summed rate, which cannot say which link was
  slow.
- **Packet level health** from the kernel: the share of retransmitted packets
  with a per-second rate, the socket table by state, and the conntrack usage
  against its limit.
- **The network topology of the device:** the default gateway, the resolvers,
  the routing table, the neighbour table, the radio quality of the wireless
  links and the tunnels.
- **The state of the host firewall** (ufw, firewalld, nftables), so a device
  with an exposed port can be checked without leaving the dashboard.
- **The container links are visible now.** They used to be dropped at the
  source, which hid the bridge that was carrying the traffic. They are
  reported and clearly marked as outside the totals, so the fleet numbers do
  not change.

### Changed

- **Every interface carries more than counters:** its kind (physical, wireless,
  tunnel, bridge, container, loopback), the driver behind it, the duplex, the
  carrier changes and the hardware level counters that separate a congested
  network from a broken cable.
- **The notifications only interrupt when there is something to act on.** A
  device that comes back online is recorded in the inbox and in the bell, but
  it no longer raises a toast: the alert stack was filling with good news.
  Only the events the catalogue marks as needing attention interrupt, and only
  while the dashboard is open — the history the dashboard reads when it opens
  is never replayed as a pile of popups.
- **The alerts lead somewhere.** Clicking a toast or a bell row opens the
  device the event is about, the stack moved to the bottom right so it stops
  covering the device readings, three alerts are shown at a time with a link
  to the rest, and the history is grouped by day.
- The device agent is now version **0.10.0** and the collection contract
  **1.8.0**. A device on an older agent keeps working: it reports fewer
  fields, and the screen says what is missing instead of showing zeroes as
  facts.

### Fixed

- **A device that came back was left invisible.** The anti-flap window
  dropped the "back online" notice when another one had been recorded a few
  minutes earlier, so the alert stayed open in the history and the device
  that recovered was never reported. The recovery notice is now recognised as
  the resolution of an alert and is never swallowed.

## [0.13.2] - 2026-10-21

### Fixed

- **Main cannot be released with a red CI.** The Release workflow now starts
  from a successful CI run on the merge commit instead of the push itself, so a
  failing test blocks the image and the tag. `main` is protected: changes go
  through a pull request with every CI check required, administrators included.

## [0.13.1] - 2026-10-21

### Fixed

- **The browser suite follows the chart-first Overview.** Its live-data checks
  still read the vitals cards that 0.13.0 removed, so the end-to-end job failed
  after the release; the checks now assert the Performance chart's CPU series
  and keep the liveness regression on the topbar and fleet row.

## [0.13.0] - 2026-10-20

### Added

- **The device Overview is a full dashboard.** The Performance chart
  plots CPU, memory and temperature with toggles, a percentage axis, a right
  temperature axis, time ticks, hover values, and a legend with each series'
  last/average/min/max; Network and Disk I/O get their own charts with the same
  numbers. Load and cores, storage, interfaces, top processes and system facts
  close the page. Disk I/O history is honest about being live-only: the
  downsampled ranges do not aggregate it.
- **Development sign-in without the login screen.** `make dev` bootstraps
  `demo@sync-win.local` / `sync-win-demo-password` and a fixed
  `SYNCWIN_DEV_TOKEN`; the dashboard trades the token for a normal session
  cookie, so the sign-in form is skipped. The server ignores the token unless
  `SYNCWIN_ENV=development` and it never creates an account.
- **A tab per terminal session in Remote access.** Up to six SSH sessions can
  be open at once, each in its own tab with its own account, ticket and
  connection. The tab strip has a `+` control, each tab is named after the
  program running in it (btop, vim, ...) and falls back to the account name, and
  closing one tab ends only that session. Switching tabs keeps every session
  alive, and a refused session reports why it never connected — including the
  device already serving its six — inside its own tab instead of replacing the
  screen.

### Fixed

- **Scrollbars match the dark theme.** Every scroll container — pages, panels,
  drawers, log viewers — uses a thin, rounded, low-contrast thumb instead of the
  browser's light bar. The terminal's own xterm slider uses the same muted range.
- **Remote access works when the dev dashboard is opened by IP or a Tailscale
  name.** The terminal WebSocket only accepted the Vite proxy's rewritten Host
  and the localhost CORS origins, so a LAN browser got "The session connection
  failed". Development now also accepts the Origin the browser reports;
  production keeps the explicit Host/CORS check.

### Changed

- **The fleet landing is a Termix-style host list.** Each device row shows CPU,
  RAM and disk as inline percentage bars, with status, OS, last-seen and the
  alert chip on the same line. The KPI wall, the per-row sparklines and the
  recent-alerts column are gone; the alert count in the summary links to
  Alerts, and detailed charts remain on the device Overview.
- **The Remote access screen is organized around the terminal.** The session
  audit moved into a scrollable side panel that matches the terminal's height,
  so it can no longer grow the page, and the fixed device actions (restart
  agent, package updates, reboot) moved to Device settings → Maintenance.
- **The full test device supports real Remote access sessions.** The Ubuntu
  container now ships sshd, a `devuser` account and the remote-access helper,
  and its local policy allows that account, so `make dev-agents-full` is enough
  to open live terminals. The image is rebuilt on every run; a stale image had
  silently skipped a Dockerfile change.
- **The browser suite no longer creates an account per run.** It signs in as
  the fixed development account, reuses one device record, and deletes it on
  exit, so running `make test-e2e` leaves the dev fleet unchanged.

## [0.12.0] - 2026-10-19

### Changed

- **Docker screen rebuilt** to match the rest of the dashboard: tiles for
  running, stopped, paused and images, and a searchable container list with
  state filters. Clicking a container opens a large side panel that slides in
  from the right with focused tabs: Overview, Stats (live CPU, memory, network
  and disk), Logs, Terminal and Config. Compose and cleanup are top-level tabs
  instead of hidden sections.
- **The container list is live.** The screen subscribes to the server's Docker
  state stream, and Refresh asks the device for a fresh list instead of
  re-reading a snapshot that can be a minute old.
- **Container actions answer in about two seconds.** The agent drains the Docker
  request queue on its own two-second poller rather than waiting for the
  telemetry cycle, and a failed command states the device's reason.
- **Pause/unpause and kill** join the existing start/stop/restart/remove actions.
  Paused containers now show a Resume action instead of an unusable Start.
- **Container overview and configuration** in the side panel: live metrics,
  health, ports, networks and mounts on Overview; platform, restart policy,
  limits, entrypoint/command and labels on Config. Environment variables are
  never sent, and credential-looking command arguments and labels are masked.
- Paused containers are shown as needing attention on the device instead of
  offering start/restart buttons that Docker would reject.

### Fixed

- Published container ports now appear. The agent decoded the Engine's
  `PrivatePort`/`PublicPort` keys into underscore-tagged fields, so every port
  arrived as zero and the screen showed "no ports".
- A compose scan now reports "no compose project found" when the device simply
  has none, instead of an error caused by a `null` reply or a duplicate empty
  state next to a real error.
- Disabled Docker mutations now answer 403 with "docker mutations are disabled on this server"
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
