---
id: context/p1-functionality
type: context
title: "P1 functionality remediation"
description: >-
  P1 connects the agent's existing collectors to the server. P1.1 preference
  synchronization and P1.2 application inventory are complete; save
  synchronization and restore remain separate follow-up capabilities.
tags: [p1, functionality, preferences, agent]
source: other
created: 2026-09-25
updated: 2026-09-25
status: active
expires: 2026-10-25
---

## P1.1 — Preference sync

### Scope

- Use the explicit default file list from the embedded collection contract.
- Add only absolute, home-relative entries from `~/.config/lem/allowed-files`.
- Reject symlinks, directories, empty/binary files, sensitive filenames and paths outside `$HOME`.
- Upload through the existing authenticated `POST /api/devices/{id}/sync` endpoint.
- Run on a separate interval from telemetry (contract default: 300 seconds).

### Observations

- The server endpoint and `SavePreferenceBatch` already existed; the missing piece was wiring the collector into the daemon.
- The original collector mutated `LastSyncHashes` before upload. That could suppress a retry after a network/server failure.
- P1.1 now advances hashes only for files returned in the server's `saved` list.
- Rejected files remain eligible for a later retry.
- The sync interval is tracked per daemon process so a failed request does not cause a request on every telemetry cycle.
- No recursive home scan was added; collection remains allowlist-based.

### Tests

- Successful first sync and persistence.
- No resend for unchanged files.
- Retry after HTTP failure.
- Rejected files do not advance hashes.
- Extra allowlist files are accepted; outside-home entries are rejected.

## P1.2 — Application inventory

### Scope

- Collect only the sources declared by `apps_inventory`: APT, Flatpak, Pacman, AUR and AppImages.
- Run on the contract refresh interval (300 seconds by default), independently from telemetry.
- Upload through the authenticated `POST /api/devices/{id}/apps` endpoint.
- Align the payload with the documented `{source, name, version, path}` shape.

### Observations

- Pacman explicit packages are filtered against `pacman -Qmq`; AUR entries come from available `paru` or `yay` helpers.
- AppImage discovery is deliberately shallow and limited to `~/Applications` and `~/.local/bin`; symlinks are skipped.
- A failed installed source aborts the whole inventory so partial data cannot replace the last complete server inventory.
- The local inventory timestamp advances only after a successful upload; failures retry on the next inventory interval.
- Empty inventories are encoded as `[]`, preserving the dashboard API contract.

### Tests

- All declared package sources, versions and AppImage paths.
- Foreign package filtering and AppImage symlink rejection.
- Partial inventory refusal when a command fails.
- Authenticated payload, empty-array encoding, retry and timestamp persistence.
- Embedded contract and server `AppInfo.path` JSON shape.
- Full `go test ./...` and `go vet ./...` pass for both agent and server modules.

### Next

- P1.3: connect save collection and restore handling.
