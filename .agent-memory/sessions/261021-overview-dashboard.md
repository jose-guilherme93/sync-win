---
id: sessions/261021-overview-dashboard
type: session
title: "Device Overview: vitals with numbers, Chart.js charts and a system grid"
description: >-
  The Overview was three gauges, a row of stat cards and a hand-drawn SVG
  history chart with no axes, tooltips or averages. It is now a dashboard with
  period statistics on every vital, three axis-labelled charts whose legends
  carry last/average/min/max, and the hardware panels that were missing.
tags: [overview, charts, chart.js, telemetry, frontend, dashboard]
source: agent
created: 2026-10-21
updated: 2026-10-21
status: active
---

## What changed

- **Data layer**: `ChartPoint` gained `disk`, `diskRead`, `diskWrite` and
  `power`; live points and raw history fill them. `lib/telemetry-history.ts`
  centralises history normalisation (aggregated buckets and raw payloads) plus
  `pointValues` (aligned, gaps kept), `seriesValues` and `seriesStats`
  (last/average/min/max of what is actually plotted).
- **`TimeSeriesChart.svelte`** wraps Chart.js (already a dependency) with dark
  axes, time ticks, an optional right axis, a crosshair tooltip and an HTML
  legend that shows each series' last, average, minimum and maximum. Units come
  from the series or the axis, so network/disk use `formatRate`.
- **Overview**: four vital cards (CPU, memory, disk, temperature) with period
  statistics and sparklines; a Performance chart (CPU/memory on the left % axis,
  temperature on the right °C axis, toggles); Network and Disk I/O charts; a
  status strip (uptime, last sample, agent impact, alert counts); and a system
  grid (load and cores, storage, interfaces, top processes, facts).
- **Honesty**: a 0 °C reading is treated as "no sensor" (the agent reports 0
  when absent), so the card shows — and the temperature series/toggle disappear.
  Disk I/O is live/raw only; longer ranges show a message saying the downsampled
  aggregates do not carry disk.
- `StatCard` gained `avg` (rendered as `ø`). `GaugeCard` had no consumer left
  and was deleted.

## The bug worth remembering

Chart.js instances for the Network and Disk charts were never created even
though their legends rendered. The reactive dependency list
(`$: if (canvas && (labels, series, …))`) reliably fired for the first chart but
not for the later instances. Replacing it with `afterUpdate(() => { if (canvas)
syncChart() })` fixed all instances. The canvases kept the Chart.js default
300×150 until then.

## Verification

- 9 normalisation/statistics tests, 2 chart tests, 3 Overview tests. Web: 30
  files / 262 tests, svelte-check 0 errors, build clean.
- E2E: 76/76; the live check now reads `[data-vital=cpu] .value` and asserts the
  chart legends expose series statistics.
- Browser: 879×854 (vitals 2×2) and 1280×900 (4 across), 1h and 7d; the 7d Disk
  I/O card shows the live-only explanation. Temperature absent shows — and a
  disabled Temp toggle.
