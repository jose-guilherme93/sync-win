---
id: sessions/261005-dashboard-redesign
type: session
title: "Dashboard redesign into a navigation-first panel"
description: >-
  Rebuilt the web dashboard around a sidebar shell, added ten screens, fixed the
  zero-timestamp and chart bugs, and corrected the project's verification gate.
tags: [web, dashboard, svelte, ui, redesign, session]
source: agent
created: 2026-10-05
updated: 2026-10-05
status: active
---

## What happened

- Fixed the `"739893d ago"` reading on Last sync. Root cause: Go marshals an
  unset `time.Time` as `"0001-01-01T00:00:00Z"`, a valid RFC3339 string that
  passed every truthiness guard. Shared `lib/format.ts` now rejects it.
- Fixed clipped Network/Temperature charts and added an empty-chart state.
- Built the app shell: `lib/theme.css` tokens, `lib/router.ts` nav store,
  `components/shell/` (Sidebar, Topbar, DeviceList) and nine `components/ui/`
  primitives. Selecting a device now scopes the main panel; nothing is a popup.
- Added the screens: Home, DeviceOverview, Alerts, Reports, Storage, Processes,
  Packages, Services, RemoteActions, DeviceSettings.
- Extracted alert logic into `lib/insights.ts` with its thresholds in one
  exported object, covered by 10 tests.
- Wired the overview period selector to the real `telemetry/history-v2`
  endpoint; it previously re-rendered the same two minutes for every range.
- Removed the legacy fleet grid and the two now-orphaned components
  (`Sparkline.svelte`, `SimpleMetrics.svelte`).
- Corrected the verification gate after it caused real misses: `vite build`
  passed on code with 16 type errors and on a Svelte attribute bug.

## Bugs caught by `npm run check` that `vite build` missed

- `<DeviceList {devices} selectedId ...>` — an attribute without braces is the
  boolean literal `true`, not the variable. Every device row got
  `selectedId={true}`.
- Literal `{id}` inside quoted attribute values in Packages/Services.
- Three conflicting copies of `Device`/`HardwareStats` across App, DeviceModal
  and SystemMetrics.

## State

Branch `feat/rigsight-dashboard`, 11 commits. `npm run check` 0 errors /
14 warnings, `vite build` 180 modules, 54 web tests, server and agent Go suites
green (run via Docker; no `.go` file changed).

## Next

See `context/dashboard-redesign.md`. The immediate one is opening the dashboard
in a browser — no screen has been seen running yet.
