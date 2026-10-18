---
id: sessions/261017-cpu-clock
type: session
title: "CPU clock and core voltage in telemetry"
description: >-
  Added the last two CPU fields the dashboard had been omitting: the current
  average clock (cpufreq, falling back to /proc/cpuinfo) and the CPU core
  voltage from hwmon when a labelled CPU rail exists. Contract and agent version
  bumped so installed agents receive it.
tags: [telemetry, cpu, hwmon, contract, agent, ui]
source: agent
created: 2026-10-17
updated: 2026-10-17
status: active
---

## What was asked

The CPU screen said clock and core voltage were not collected. The roadmap
tracked them as known backend gaps.

## What was built

Worktree `sync-win-cpu-clock`, branch `feat/cpu-clock`.

### Agent

- **`collectors/cpu_clock.go`** — `CollectCPUClockMHz` averages the current
  frequency across logical CPUs. It tries `cpu*/cpufreq/scaling_cur_freq` (what
  the governor asked for), then `cpuinfo_cur_freq` (what the hardware reports),
  then the `cpu MHz` lines in `/proc/cpuinfo`. Values are kHz in sysfs, so they
  are divided by 1000; `0` means no source is available.
- **`collectors/core_voltage.go`** — `CollectCoreVoltageV` scans
  `hwmon*/in*_input`, requires a sibling `in*_label` naming a CPU rail
  (`vcore`, `core`, `cpu`, `vddcr`, `svi`), and only accepts 0.4–2.5 V. The
  label filter matters: the first voltage channel on a motherboard is usually
  +12 V, and reporting that as core voltage would be worse than nothing.
- Both run in the existing parallel collector group and fill
  `cpu_clock_mhz` / `core_voltage_v` (`omitempty`, so a host without the source
  sends neither).
- Contract `1.6.0` → `1.7.0`, agent `0.7.2` → `0.8.0`, and the two names were
  added to `telemetry.fields` and `COLLECTION-CONTRACT.md`. The version bump is
  what makes installed devices fetch the new binary.

### Server / web

- `HardwareStats` and `HardwareSummary` carry both fields; the summary decodes
  them straight from `hardware_json`, so no migration or extra column.
- `CpuThermal.svelte` adds CPU clock and core voltage tiles (GHz above 1000 MHz)
  and the same rows in the Processor card. Missing sensors render `—` with a
  reason, not a fabricated number.
- `types.ts` marks both optional.

## Verification

- New collector tests with temp sysfs/procfs fixtures: cpufreq wins over
  cpuinfo, averaging is correct, cpuinfo fallback works, missing sources return
  0; voltage accepts Vcore, ignores +12 V/+3.3 V, rejects an implausible
  Vcore reading and returns 0 when there is no sensor.
- `go test ./collectors/...` and a full agent build pass.
- On the dev host (no cpufreq, no hwmon): `clock=3408.0 MHz voltage=0.000 V`,
  from `/proc/cpuinfo`. Voltage is covered by fixtures because this host has no
  voltage sensor.
- Web `svelte-check` / build run with the rest of the branch; no new component
  test (CpuThermal has none).

## Not done

- Per-core frequency and throttling counters: only the package average is
  collected.
- AMD SMU / Intel RAPL voltage rails are not read; hwmon is the only source.
- GPU clock/voltage remain part of the NVIDIA collector that is still pending.
