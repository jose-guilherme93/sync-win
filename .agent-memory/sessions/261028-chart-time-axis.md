---
id: sessions/261028-chart-time-axis
type: session
title: "Charts on a real time axis, with Beszel's plot look"
description: >-
  Telemetry charts used a category axis, so a sparse window stretched its few
  samples across the whole card and a spline invented peaks at the start of a
  series. Every plot now shares one linear time axis, a fixed window, filled
  areas and Beszel's palette.
tags: [charts, chart.js, telemetry, frontend, dashboard, beszel]
source: agent
created: 2026-10-28
updated: 2026-10-28
status: active
---

## What changed

- **`web/src/lib/chart.ts`** is the single source of the plot geometry:
  `timeDomain` (fixed window anchored on the newest sample or the fetch end),
  `buildTimeTicks` (round 5s–30d steps instead of Chart.js' arbitrary numeric
  ticks), `formatTimeTick` (seconds for a minute window, dates past 36h),
  `formatTooltipTime` and `withAlpha`.
- **`TimeSeriesChart.svelte`** switches the x axis from `category` to `linear`
  over epoch milliseconds and takes `windowMs` / `windowEnd` props. The data
  is `{x, y}` per sample, interpolation is monotone (no overshoot) and the
  fill is a vertical gradient. This is the one plot used by the Overview, the
  Network traffic chart and the System tab.
- **Overview** passes the selected range as the window and the fetch's `to`
  as its end. Live uses the 60 s window anchored on the newest sample.
- **Network** builds its per-interface stack as `ChartSeries` and renders the
  shared chart instead of a second hand-written Chart.js config; the history
  fetch records its `from`/`to` for the window.
- **SystemMetrics** lost its four direct Chart.js instances and renders four
  `TimeSeriesChart`s, with a window of `refreshInterval × MAX_POINTS`.
- **Palette** lives in `theme.css` as `--chart-1…5` and `--chart-*` surfaces,
  copied from Beszel's dark tokens (hsl 220/160/30/280/340 at 70–80%
  saturation). CPU is blue, memory green, temperature and disk writes orange,
  upload pink. Hex is deliberate: canvas gradients cannot parse `hsl()`.

## Why the fixed window matters

The old category axis gave every sample the same width regardless of the time
between samples. Two points an hour apart filled the card exactly like two
points a second apart; a downsampled gap looked like real, even sampling.
Fixing the domain to the selected window is what makes the line grow from the
left as data arrives, leaves an honest blank where no samples exist, and keeps
the distance between ticks proportional to time.

## Verification

- `npm run test`: 311 tests, including new `chart.test.ts` for domain, tick
  alignment and alpha.
- `npm run check`: 0 errors. `npm run build`: clean.
- Browser (Vite against the dev stack): Overview in Live, 1h and 7d; Network
  24h. The 1h chart with 15 minutes of history shows data only in the last
  quarter instead of stretching it; Live shows 15 s ticks; the tooltip reads
  "Oct 25, 01:05 PM" with both series. Screenshots in `/tmp/opencode`.

## What shipped

PR #19 (the chart work) and PR #20 (its release notes), released as **0.14.1**
on 2026-10-28: Docker Hub and GHCR tagged `0.14.1`, `v0.14.1` and `latest`,
tag `v0.14.1`, GitHub Release body read from `## [0.14.1]`. The agent version
stays at 0.10.0 because nothing under `agent/` changed; a web-only release is
not an agent update.

## The lesson

The chart change carried only an `## [Unreleased]` entry, so the first Release
run on the merged commit stopped after 13 seconds with
`CHANGELOG.md has no section for 0.14.1` — the workflow reads the notes for the
version it just computed *before* it publishes, which is exactly the guard
working. Nothing was tagged, so the fix was another PR that moved the entries
into `## [0.14.1]`; the next green CI run on `main` published it.

Write the section for the version the bump will produce **before** the PR is
merged, and compute that version from the commits since the last tag rather
than from the one commit you are writing. `[Unreleased]` is not a substitute.
