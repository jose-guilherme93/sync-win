---
id: sessions/261021-processes-tab
type: session
title: "Processes tab: full state tally, diagnosable rows, cheaper collection"
description: >-
  The Processes tab was two top-ten lists with PID, name, CPU and RSS. It now
  shows the state of every process, the reported consumers and every
  stopped/zombie row with user, command, threads and age, and filters a
  sortable table; the agent reads one stat file per process instead of three.
tags: [processes, agent, collector, ui, performance, contract]
source: agent
created: 2026-10-21
updated: 2026-10-21
status: active
---

## What changed

- **Agent.** `CollectProcesses` reads `/proc/<pid>/stat` once per process and
  derives comm, state, PPID, CPU jiffies, threads, start time, virtual memory
  and RSS from that single line. It counts every state into `process_summary`
  and returns four capped lists: top CPU, top memory, stopped (`T`/`t`) and
  zombies (`Z`), the last two ordered oldest first. Only the union of those
  rows pays for `/proc/<pid>/status` (uid) and `/proc/<pid>/cmdline`; `/etc/
  passwd` is read once. That is cheaper than the previous comm+status+stat pass
  per process, despite carrying much more information.
- **Caps** live in `agent/internal/contract/contract.json` under `processes`
  (`max_top` 20, `max_problem` 10, `max_cmdline_chars` 200) and are documented
  in `COLLECTION-CONTRACT.md`.
- **Server** carries the new fields through `store.ProcessInfo`,
  `store.ProcessSummary` and `HardwareStats` (the telemetry handler re-marshals
  the typed payload, so unknown fields would otherwise be dropped).
- **Processes screen.** Seven summary tiles (total/running/sleeping/disk
  sleep/stopped/zombie/threads), an alert strip when a problem state exists,
  state filters with counts, search, sortable headers, a table with PID/PPID,
  name + command, user, state, CPU share, RSS + % of RAM, threads and age, and
  a detail panel with the full command, virtual memory and the meaning of the
  state. "N reported of M total" is always visible; an older agent without the
  tally is labelled as showing the reported subset.
- **CPU semantics stay machine-share** (delta jiffies divided by cores), but
  the screen states it next to the number and shows threads separately, so a
  multithreaded process is not mistaken for a small one.

## Product standard recorded in AGENTS.md

Added "Device tabs: the information standard": every tab must say what the
subject is, whether it is healthy, what it costs, what needs attention, the
scope of the data (whole machine vs reported sample), and must distinguish
absent/not-reported/zero. Detail must not add a collector pass or per-row
browser work. The Processes tab is the reference implementation.

## Verification

- Agent: parser test (comm with spaces/parens), first-sample zero CPU, delta
  path, state-summary invariant, problem-list states, command truncation.
- Web: 6 Processes tests (tally, dedup, filters, search, detail, legacy agent
  fallback); 31 files / 268 tests, svelte-check 0 errors, build clean.
- E2E: 77/77, including seeded process data, sort control, state filters and
  the whole-machine tally.
- Live: created a stopped `sleep` and a zombie `perl` in the test container;
  both appeared with user, command, threads and age, the Problems filter showed
  exactly them, and the detail panel explained the stopped state. A CPU burner
  appeared at ~12.5% on four cores (~50% of one core).
