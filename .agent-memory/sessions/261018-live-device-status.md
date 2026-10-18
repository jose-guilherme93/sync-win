---
id: sessions/261018-live-device-status
type: session
title: "Device screens were frozen: live summary merged into the detail, heartbeat poll decoupled"
description: >-
  Home reported devices online while the topbar and device screens said stale,
  and the Live/refresh controls did nothing on those screens. The selected
  device preferred a /detail snapshot loaded once on selection, the poll that
  was supposed to refresh it was gated by a flag that never became true, and the
  chart range also owned the heartbeat poll.
tags: [ui, status, telemetry, polling, bug]
source: agent
created: 2026-10-18
updated: 2026-10-18
status: active
---

## What was reported

- The fleet Home and sidebar showed every device online, but the topbar and the
  device screens showed `stale`/`offline` for the same devices.
- The topbar Live/range buttons and the manual refresh did not change anything
  on several device screens.

## Root cause

1. `App.svelte` computed `modalDevice = deviceDetails[id] || devices.find(...)`.
   `deviceDetails` is the `/detail` payload loaded when the device is selected;
   everything the device screens and the topbar render came from it.
2. `pollTick` refreshed that detail only when `deviceModalOpen` was true, but
   the inline shell never set it to true (the only assignments were
   `= false`). The snapshot therefore froze at selection time: `last_seen_at`
   crossed the 30s stale threshold and then the 5min offline threshold while the
   polled list stayed fresh.
3. The manual refresh called only `loadDevices()`, which updated the list — but
   `modalDevice` still preferred the frozen detail, so the button looked broken.
4. The topbar range selector owned `POLL_MS`. Selecting 7d/30d dropped the
   device poll to 5 minutes, so status and telemetry were old by construction.
5. `DeviceOverview.loadHistory` fetched a range once and never refreshed it; only
   the Live window had a live path, through the telemetry store.

## What was built

Worktree `sync-win-live-device`, branch `fix/live-device-status`.

- **`lib/device.ts` + test** — `mergeDeviceDetail(detail, live)` spreads the
  fresh list summary over the detail snapshot and merges hardware field by
  field. Detail-only keys (`top_cpu_processes`, `docker_containers`, `logs`,
  `securityPosture`) survive because the summary does not carry them. Unit tests
  pin the lifecycle/scalar precedence and the preserved heavy fields.
- **`App.svelte`** — `modalDevice` is the merge; `deviceModalOpen` and the dead
  `closeDeviceModal` are gone. The detail snapshot refreshes every 60s while a
  device is selected. The manual refresh forces both the list and the detail
  (with posture when on the Security section). The device/heartbeat poll is a
  fixed 10s; the chart range only publishes the window.
- **`DeviceOverview.svelte`** — schedules its own history refresh from the
  range's `refreshMs`; Live still uses the telemetry store and needs no timer.
- **Topbar / `timerange.ts`** — copy now says the range controls the chart
  refresh cadence, not the heartbeat poll.

## Verification

- `npm run test`: **21 files / 161 tests** (3 new), `svelte-check` 0 errors,
  `vite build` clean.
- The merge is unit-tested. The live check (open a device for 2+ minutes and
  confirm status/gauges do not freeze; switch Live/1h/24h; press refresh) is
  left for the operator against the dev stack, because agent-browser is not
  installed on this host.

## Follow-up (same day)

Switching devices while a screen was open reused the component. `DeviceOverview`
subscribed to `deviceHistoryStore(device.id)` only at creation, so the Live chart
kept the previous device's history, and the sparkline series in CpuThermal,
Memory, Storage, Network and Sensors keyed samples only by `collected_at`, so
they appended the new device's points to the old device's series. Both now reset
on a `device.id` change (the overview re-subscribes; the screens clear their
series and sampling stamp).

## Not done

- No end-to-end test for "stays online while a device keeps heartbeating"; the
  e2e suite seeds a device without a live agent.
- The stale/offline thresholds remain 30s/5min. With a 10s poll, a device that
  stops is detected within one threshold, not instantly.
- `formatRelative` still re-renders every 15s from the app clock; status itself
  now follows each poll.
