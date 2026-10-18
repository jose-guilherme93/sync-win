---
id: sessions/261018-agent-housekeeping
type: session
title: "Agent housekeeping: log noise, old-install remnants and fingerprint adoption"
description: >-
  The agent wrote three routine journal lines every ten seconds, the installer
  left one binary/unit backup per run (~55 MB on a real device), LEM-era units
  and directories were never removed, and the installer did not send the
  fingerprint the server needs to adopt a machine-less device record.
tags: [agent, logs, cleanup, installer, fingerprint, telemetry]
source: agent
created: 2026-10-18
updated: 2026-10-18
status: active
---

## Measurements (dev stack, 2026-10-18)

- A full device's telemetry payload reached **~40 KB** on log cycles:
  `logs` 27 KB, `docker_containers` 10.4 KB, top processes 1.6 KB. The running
  average was 6.9 KB per sample, posted every 10 s.
- The agent journal produced **18 lines/minute** on a healthy device:
  `cycle started`, `lynis found at ...` and `telemetry sent ...` every 10 s.
  Those lines are shipped back to the server as device logs, so the Logs screen
  was mostly this loop.
- The real host had **five `sync-win-agent.bak.*` binaries and two
  `sync-win-agent.service.bak.*` units** (~55 MB). `backup_existing` created
  them for rollback and never removed them after success.
- Legacy LEM names: `lem-agent.service`, `lem-agent-update.service`,
  `lem-agent-update.timer`, `/usr/local/bin/lem-agent`, the `lem` user and
  `/var/lib/lem`, `/var/log/lem`, `/etc/lem`. The old unit ran as `lem` with
  those read-write paths.

## Changes

Three branches, all based on `develop`:

1. **`fix/agent-noise`**
   - Lynis availability is logged only on a transition (path included), not
     every cycle. `lynisState` has a unit test for first/unchanged/changed.
   - The telemetry summary is logged once per log-collection cycle (~60 s);
     errors still log every cycle.
   - `cycle started` is gone. The first cycle still reports immediately
     (`logCycle` starts at the collection limit).
2. **`fix/agent-cleanup`**
   - The installer removes its rollback backups on success and after a
     rollback, and deletes `.bak.*`/`.rollback` leftovers from older runs.
   - Installer and root updater remove LEM units/binaries/dirs/user; the old
     `policy.json` is migrated to `/etc/sync-win/policy.json` first.
   - The updater's backup-glob cleanup is unit-tested with temp paths.
3. **`feat/agent-fingerprint`**
   - `sync-win-agent fingerprint` prints the SHA-256 fingerprint.
   - The installer runs the freshly installed binary and includes the
     fingerprint in the enrollment payload.
   - `EnrollDevice` adopts an existing record when no machine id matches but a
     machine-less record of the same owner carries the same fingerprint, and
     records the new machine id. Machine-bound records and other owners are
     never merged (tests).
   - Agent `0.8.0` → `0.9.0` so installed devices fetch the new binary.

## Fingerprint analysis

- The fingerprint is `SHA-256(cpu model, RAM kB, primary MAC)`. It is stable
  across reinstalls but can change when the primary NIC changes or the MAC is
  randomised; it is metadata, never an authenticator.
- Enrollment adoption is by `/etc/machine-id` first. Records created before
  machine ids existed have `machine_id = ''`; if they also have no fingerprint
  (LEM-era) nothing can safely match them, so they stay offline and should be
  deleted by the operator. Records with a fingerprint are adopted now.
- `checkDuplicateFingerprints` still marks an older device `duplicate` when two
  devices report the same fingerprint. That remains a heuristic for cloned
  hardware; adoption removes the common reinstall case.

## Verification

- Agent and server `go test ./...` green; `golangci-lint --new-from-rev` clean
  on both modules; `bash -n scripts/install.sh` clean.
- The backup cleanup is unit-tested. The installer/updater system-path cleanup
  needs root and a LEM-era device to exercise; it is written to no-op when the
  paths are absent.
- The log-noise reduction is verified by the next dev image rebuild and the
  test devices (agent 0.9.0).

## Not done

- `docker_containers` is still 10.4 KB every cycle. The Docker screen reads the
  cached list from `/api/devices/{id}/docker`; moving it to an on-demand `list`
  command needs UI work. Tracked in ROADMAP under FASE 21.
- Static telemetry fields (OS, kernel, CPU model, boot time, ...) are ~150 bytes
  together; not worth a partial-payload protocol.
- Top processes are still sampled every cycle (~1.6 KB).
