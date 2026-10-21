---
id: sessions/261021-fleet-list
type: session
title: "Fleet landing: a dense host list with inline usage meters"
description: >-
  The fleet Home carried a KPI wall, a wide table with per-row sparklines and a
  recent-alerts column. It is now a Termix-style list of hosts with inline CPU,
  RAM and disk percentage bars; charts stay on the device Overview.
tags: [fleet, home, ui, frontend, charts, redesign]
source: agent
created: 2026-10-21
updated: 2026-10-21
status: active
---

## What changed

- `Home.svelte` is a list of device rows, each a button: status dot, hostname,
  OS / last seen / tags, three inline meters (CPU, RAM, disk) and the alert chip.
  The whole row selects the device.
- Meters use the same thresholds as `generateInsights` (`THRESHOLDS`), so a bar
  can never read green while the detail page raises an alert. The disk meter
  prefers `/`, falling back to the largest partition (containers report the host
  disk under `/etc/hosts`); the mount is in the `title`, the visible label is
  just "Disk".
- The five KPI cards and the "Recent alerts" column are gone. A one-line summary
  (`N devices · X online · N alerts ›`) links to the Alerts screen; `App.svelte`
  passes `onAlerts={() => nav.setSection('alerts')}`.
- `DeviceRow.svelte` and its test are deleted — Home was their only consumer.
  `MiniSparkline` stays for the device screens.
- The mobile layout stacks the meters under the identity and alert chip below
  860px.

## The bug worth remembering

The first version named the meter classes `.meter` and `.track`. `app.css` had a
legacy global `.meter { height: 4px }` from the old fleet bars, so every meter
collapsed to the 4px track and its label/value overflowed outside the box. The
fix renamed them to `.usage-*` and deleted the dead global `.meter` block.
Scoped Svelte CSS does not protect against global class collisions when the same
class name is used deliberately.

## Verification

- `Home.test.ts`: 6 tests (inline percentages and bar widths, no SVG in the
  list, row selection, alert link, critical-first sort, add, empty state).
- Web: 27 files / 248 tests, svelte-check 0 errors, build clean.
- E2E: 75/75, including "fleet list rendered", "three inline meters", "no
  time-series chart" and the updated row selector.
- Browser: checked at 879×854 and 400×800; no horizontal overflow, the meters
  render at 20px with labels and values inside the row.
