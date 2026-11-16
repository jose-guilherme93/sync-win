---
id: sessions/261109-storage-tab
type: session
title: "Storage tab: filesystem and disk health with real history"
description: >-
  The Storage tab showed capacity and two sparklines. It now answers what a
  sysadmin asks: which mount fills first, whether inodes are the limit, how fast
  it grows, and which physical disk is behind it — with per-disk throughput,
  latency, temperature and SMART, plus a daily rollup that makes the forecast
  real instead of extrapolated from two hours.
tags: [storage, agent, contract, dashboard, history, smart]
source: agent
created: 2026-11-09
updated: 2026-11-09
status: active
---

## What changed

- **The partitions payload gained the fields that decide an intervention**:
  `fstype`, `options`, `read_only`, `avail_bytes` (what an unprivileged process
  can still write), `reserved_bytes` and the inode counts. Inode exhaustion is
  its own limit; a filesystem can be "71% used" and still refuse a write.
- **`disk_devices[]`** covers the physical side: model, serial, rotation,
  removable, temperature from the kernel hwmon, and counters from
  `/proc/diskstats` turned into per-interval `read_rate`/`write_rate`/`iops`/
  `await_ms`/`util_percent` by `collectors.DiskDeviceReader`, which keeps the
  previous sample. SMART is read with `smartctl` at most every 15 minutes and
  caches its failures: `available:false` with a `reason` is the normal state on
  an unprivileged agent and is never shown as healthy. The self-test log rides
  along as `tests[]`.
- **`storage_daily`** keeps one row per device, mount and UTC day, written
  best-effort from the telemetry ingest and pruned past 400 days.
  `GET /api/devices/{id}/storage/history?days=` serves it; the screen derives
  growth per day and the "full in N days" forecast from it, and says "stable"
  instead of a date when a mount stopped growing.
- **The screen is monochrome and long on purpose**: verdict strip, attention
  list, the full filesystem table, history chart, growth table, live throughput,
  per-disk latency, the disk table with SMART, the self-test log, and mount
  state. Severity is weight and a filled flag, not colour.

## Not done

- LVM thin pools, Btrfs/ZFS scrub, deleted-but-open files and `docker system df`
  are named on the screen as not collected, because each needs a privileged or
  additional read the agent does not do yet.
- SMART stays unavailable on a default install: the agent is unprivileged and
  `smartctl` needs raw device access.
