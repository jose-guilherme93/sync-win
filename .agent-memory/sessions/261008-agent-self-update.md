---
id: sessions/261008-agent-self-update
type: session
title: "Making agent fixes actually reach devices"
description: >-
  Two released Logs fixes left production unchanged because the agent could not
  read the journal and had no working update path. Adds daemon-driven updates
  through a systemd path unit, and records the verification rules that would
  have caught it.
tags: [agent, updates, systemd, journal, logs, session]
source: agent
created: 2026-10-08
updated: 2026-10-08
status: active
---

## What happened

Worktree `/home/joseti/projects/sync-win-agent-update`, branch
`fix/agent-self-update`, based on `develop` @ `950afeb`. The worktree was created
at the user's explicit request after three sessions of working in the main
checkout — now recorded as a rule in `AGENTS.md`.

## The diagnosis, and why the earlier ones missed

Production showed no device logs. The telemetry payload had **no `logs` key at
all**, which means nothing was ever sent — not that the server lost it.

```
asrock-ubuntu-server | agent_version=0.6.1 | logs key: False | logs_status: None
ubuntu-josetilabs    | agent_version=0.6.1 | logs key: False | logs_status: None
```

Earlier sessions had diagnosed "the server overwrites `hardware_json` and erases
the batch". That was a real defect and worth fixing, but it was not the reason
the screen was empty. The verification that "proved" the fix seeded 600 rows
directly into `device_logs`, bypassing the agent entirely. The screen rendered
correctly against data the real pipeline could never produce.

## Three causes

1. **Journal permission.** `0640 root:systemd-journal`; the unit granted only
   `docker`. Confirmed indirectly by journald's own notice that only
   `adm`/`systemd-journal` members see the full journal.
2. **Binary-only updates.** A unit change could never reach a device.
3. **No trigger at all.** `sync-win-agent-update.timer` was `not-found`.

Cause 3 is why cause 2 was fatal, and 2 is why cause 1 was permanent.

## What was built

The daemon cannot replace its binary or unit, so it asks:

- daemon writes `/var/lib/sync-win/update-request`
- `sync-win-agent-update.path` fires `sync-win-agent-update.service` as root
- that service reconciles binary **and** unit every run

Plus: credentials from the unit rather than the state file (root has a different
`HOME`), missing templates are a hard failure, `verify_agent_readiness`
regenerates a missing unit, and a journal permission error requests a unit repair
once an hour.

## Bugs found while building, worth remembering

- `refreshUnitFile` swallowed **every** fetch error, so a broken unit refresh
  would pass unnoticed — the same silence that caused the incident. Now an
  unsupported endpoint is tolerated and a real failure is reported.
- The repair throttle condition I first wrote,
  `!unitRepairRequested && (unitRepairRequested || …)`, can never be true. I
  caught it by eye only after leaving a non-compiling tree.
- My own test for the request file did not call the production function; it
  reimplemented the write. Refactored to `writeUpdateRequestTo` so the test
  exercises real code.

## Verification

- `go build`, `go vet`, `gofmt` clean on both modules
- Full server and agent suites green
- `golangci-lint --new-from-rev`: 0 issues, run as CI does

## Not done, and the honest caveat

- The `.agent-memory` files are written but the work is **not committed**.
- **`sudo -u sync-win journalctl -n 3` was never run.** The whole permission
  diagnosis is inferred, not observed. Two releases were shipped partly on the
  strength of it.
- The path-unit mechanism has **never run on real systemd**; it is covered by
  unit tests only.
- `/etc/systemd/system/sync-win-agent.service` is missing on the inspected
  device (systemd reports `Loaded: not-found`, `Active: active`) and nothing
  established what deleted it.
