---
id: sessions/261006-system-inventory
type: session
title: "First backend item: systemd services and listening ports"
description: >-
  Implemented the system inventory the Services screen needs, closing the WIP
  contract block and the first of the backend items the redesign left open.
tags: [backend, agent, server, contract, services, ports, session]
source: agent
created: 2026-10-06
updated: 2026-10-06
status: active
---

## What happened

The audit of backend pendencies confirmed that none of the items listed under
"Next steps" in `context/dashboard-redesign.md` existed in code. The roadmap had
no unchecked boxes, so these were tracked only in a memory file — now they are
also an explicit section in `ROADMAP.md`.

Closed the first item: systemd services and listening ports.

- Finished the pre-existing WIP: `contract.json` already carried a
  `system_inventory` block and `contract.go` had four accessors, none referenced
  by anything.
- Agent collector for both sections, bounded by the contract's timeouts and caps.
- Wire the daemon: `-system-interval` flag, state field, upload per cycle.
- Server storage as `services_json` / `ports_json` on the device row, mirroring
  the existing `apps_json` pattern, added by an idempotent migration.
- Three endpoints, with the split between agent auth and dashboard auth.
- Contract coverage, docs, and 22 new tests.

## Decisions worth remembering

**Omitted vs empty.** Both request sections are `*[]T`. An omitted section keeps
the previous snapshot; an empty array replaces it. Without this, a broken
`systemctl` would look identical to a device that reports nothing, and the
dashboard would clear a good inventory every time a tool hiccupped.

**Status mapping lives in the agent.** `systemctl list-units` columns are
`UNIT LOAD ACTIVE SUB`, so a healthy service reports `ACTIVE=active` with
`SUB=running`. Filtering the ACTIVE column against the contract's `running` value
discards everything. `unitStatus()` maps onto the buckets the contract and
dashboard speak, and the raw columns are kept in the payload.

**Sorting by urgency.** Units sort `failed` → `running` → `stopped` before name
order. Alphabetical order buries a broken unit among hundreds of stopped ones.

**Missing tool is not a failure.** `systemctl` or `ss` absent returns an empty
list, following `CollectApps`. An installed tool that fails is reported. The
split matters because a missing binary would otherwise log an error every five
minutes forever.

## Verification

All three suites pass. Go is not on the host; `docker run golang:1.25` is the only
runner.

- server: `gofmt -l` empty, `go vet`, `go test ./...` — all packages ok
- agent: same, all packages ok
- web: `vite build` clean, `npm run check` 0 errors / 14 warnings, 54 vitest tests

## Defects found by the tests written alongside this work

Nine, all mine, none escaped. Listed because the pattern is the useful part:

- filter on the wrong systemd column (dropped every unit)
- `ss` parser matching the process name on an already-truncated line
- `range` over values in `normalizeServices`, which never mutated
- `ADD COLUMN` without a default leaving NULL that failed `Scan(&string)`
- generic type parameter on a method, plus two forgotten call sites
- handler called with two arguments where it needed three
- two test fixtures that created a user after looking it up

The consistent thread: I wrote whole files before compiling. Building per file
would have cut the iterations.

## Next

- The remaining backend items, now in `ROADMAP.md`: `smart`, `updates`,
  `alerts` + ack, `PATCH /devices/{id}`, `POST /devices/{id}/actions`, CPU
  clock and voltage, `gpu_nvidia.go`.
- The dashboard still renders "not collected yet" for these endpoints, naming
  each missing route. `Services.svelte` is now wrong in the other direction: the
  endpoints exist and the screen should consume them.
- Still never opened in a browser. Ten screens type-check and none has been seen
  running.