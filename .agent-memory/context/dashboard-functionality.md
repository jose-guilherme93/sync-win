---
id: context/dashboard-functionality
type: context
title: "Dashboard functionality: every visible control does something real"
description: >-
  Turned the dashboard's already-present controls into working flows: Lynis
  command tracking, device settings, remote actions, pending updates, plus a
  real Findings screen and a hardened agent download.
tags: [dashboard, functionality, lynis, settings, remote-actions, updates, download]
source: agent
created: 2026-10-07
updated: 2026-10-07
status: active
---

## Goal

Do not remove controls that do not work; make each one perform the action it
announces, or show the real error. No arbitrary remote shell: explicit command
types, local agent policy, confirmation for destructive actions, docs/contract
updates where the product direction changed.

## What was completed

### Lynis audit

- `GET /api/devices/{id}/commands/{id}` now authenticates the owning dashboard
  session as well as the device token; the device token still works for the
  agent. Previously only the device token could read a command, so the
  dashboard could never observe its own queued audit.
- The server persists the Lynis report **before** marking the command
  `completed`, so the UI's completion signal implies the report is stored. A
  bad report returns 500 and leaves the command `queued`.
- `SecurityTab.svelte` polls the command, then reloads audit history. It shows
  agent-reported failure messages, a distinct queued/offline state, and no
  longer reports a timeout when the agent returned an error.

### Device settings

- New device columns (idempotent migration): `display_name`, `tags_json`,
  `collection_interval_seconds`.
- `PATCH /api/devices/{id}` (owner session only) and
  `GET /api/devices/{id}/settings` (owner session or device token).
- The agent refreshes the interval from the settings endpoint after each
  telemetry send and applies it to the next cycle. Allowed intervals come from
  the contract: 5, 10, 30, 60 seconds. A failed read keeps the current interval.
- `DeviceSettings.svelte` loads and saves for real; no more "validated — not
  saved".

### Remote actions

- `POST /api/devices/{id}/actions` accepts only `restart_agent`,
  `update_packages`, `reboot_device`. It requires the global
  `SYNCWIN_ENABLE_REMOTE_MUTATIONS` flag and refuses a duplicate pending action.
- Agent execution is allowlist-based with fixed argument lists. All three are
  disabled by default in local policy (`allow_restart_agent`,
  `allow_package_updates`, `allow_reboot_device`). Reboot uses
  `shutdown -r +1` through the same root/passwordless-sudo path as installs, so
  the result can be reported before the machine goes down. Restart requires a
  systemd-managed service and exits after reporting so systemd restarts it.
- `RemoteActions.svelte` queues, polls the command, and shows the real
  result (success, agent policy refusal, server flag off, device offline).

### Pending updates

- `agent/collectors/updates.go` runs read-only queries only (`apt list
  --upgradable`, `flatpak remote-ls --updates`, `pacman -Qu`, `paru/yay -Qua`),
  bounded by timeout and output size. It returns an explicit status:
  `ready` / `unsupported` / `error`. A missing manager is `unsupported`, never
  an empty list.
- New `updates_json` device column; `GET`/`POST /api/devices/{id}/updates`. The
  server re-validates status, sources, names and versions, and drops `updates`
  when status is not `ready` so a failure cannot read as "up to date".
- `Packages.svelte` renders the four states and the update list.

### Chart rendering

Two independent bugs made the trend charts invisible on the dark theme:

- **Canvas colour.** `MiniSparkline` is a canvas and received colours as
  `var(--series-1)` / `var(--accent)`. The canvas 2D API does not resolve CSS
  custom properties; the assignment is ignored and the context keeps its default
  black. New `lib/color.ts` (`resolveCssColor`) resolves a `var()` against the
  document root before it reaches `strokeStyle`/`fillStyle`.
- **Hidden reactive dependency.** `DeviceOverview` computed its series with
  `seriesOf('cpu')`, and Svelte's reactive analysis does not see `windowed`
  because it is only read inside the called function. The series were evaluated
  once while `windowed` was empty and never re-ran, so the History SVG had
  `points=""` and the overview sparklines never appeared. `seriesOf` now takes
  `windowed` as an explicit argument.

The Home fleet sparklines were also anonymous: `DeviceRow` drew both the CPU and
the memory trend inside the CPU cell with no label. It now takes a `metric` prop,
renders one labelled trend per column ("CPU trend" / "RAM trend") with an
accessible name, and Home places each in its own column.

### Findings vs Alerts

- `findings` no longer renders the Alerts screen. New `Findings.svelte` reports
  device state and configuration (sync error, offline, stale, duplicate
  identity, missing Lynis, outdated agent, no telemetry). Alerts keeps the
  resource thresholds. The false "Offline" rule row was removed from Alerts.

### Agent download hardening

- `handleAgentDownload` uses `http.ServeContent`: correct `Content-Length`,
  streaming, and HTTP `Range` (resumable). It no longer buffers ~10 MB in
  memory.
- The gzip middleware only compresses textual/JSON content; binary downloads
  pass through untouched.

## Verification

- `server` and `agent` `go test ./...`: all packages ok (Docker `golang:1.25`).
- `web`: 81 Vitest tests, `svelte-check` 0 errors (14 pre-existing a11y
  warnings), `vite build` clean.
- Live dev stack: `Accept-Ranges: bytes`, `Range` → 206, no gzip on the binary.

## Still open

- `Storage` SMART half has no collector; it stays honestly labelled.
- Thresholds are still computed in `lib/insights.ts`, `DeviceModal.svelte` and
  `SystemMetrics.svelte`; they should share one source.
- `cpu`, `memory`, `network`, `sensors` all render `DeviceOverview`.
- Fleet contrast/sparkline layout and the "Outdated agents"/"Without Lynis"
  labels still need review.
- Browser visual validation of the changed flows.
- `npm ci` reports one high-severity dependency advisory; not auto-fixed.
- Nothing is committed yet; the work sits uncommitted on `develop`.
