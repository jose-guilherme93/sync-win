---
id: context/p1-functionality
type: context
title: "P1 functionality remediation"
description: >-
  P1 connects the agent's existing collectors to the server. Preference sync,
  application inventory, save synchronization, policy-gated restore and
  automatic agent updates are complete.
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

## P1.3 — Save synchronization and restore

### Scope

- Discover only the contract roots and operator-configured extra directories.
- Enforce extension, directory, depth, per-file and per-cycle limits.
- Send text as UTF-8 and binary files as base64 in request-sized chunks.
- Keep save hashes separate from preference hashes and advance them only after server confirmation.
- Queue restore only for the selected Wine prefix and game, with the selected files in the command payload.

### Observations

- The server now persists `files.encoding`; hashes and displayed sizes use decoded bytes.
- Restore remains fail-closed: the server mutation flag and local `allow_restore_saves` policy are both required.
- The agent rejects absolute/traversal paths, invalid encodings, oversized payloads and symlink components.
- Restore writes through a same-directory temporary file followed by rename.

### Tests

- Collector roots, extras, filters, depth, total cap and symlink rejection.
- UTF-8/base64 classification, unchanged-file skipping and request chunking.
- Atomic restore, binary restoration, traversal rejection and symlink-parent rejection.
- Server encoding persistence, invalid base64 rejection, game filtering and restore payload.
- Full agent/server Go tests and vet pass.

## P1.4 — Automatic agent updates

### Scope

- Check the configured server version every 15 minutes through systemd timers.
- Download the agent only when the remote version is newer.
- Verify the server SHA-256 checksum and the downloaded binary version.
- Replace atomically with backup, restart, health check and rollback.

### Observations

- System-wide installations use a root updater unit; user installations use `systemctl --user`.
- The desktop agent is deployed as a user service for `joseti`, with `HOME` and `XDG_STATE_HOME` pointing at that user's real paths.
- The live deployment was verified with a controlled `0.6.1 -> 0.6.2` upgrade, then restored to the server-deployed `0.6.1` binary.
- The system-level `lem` user is reserved for headless/system-only deployments; it must not be used to read another user's home.

### Tests

- Version comparison and parsing.
- Checksum/download/replace/rollback unit test with mocked systemd.
- Real user-timer execution reporting the current version.
- Real controlled upgrade and health check, followed by restoration.

## P1.5 — Package installation actions

### Scope

- Replace the agent's `install_app` stub with fixed, source-specific package commands.
- Support APT, Flatpak, Pacman and AUR helpers while refusing AppImage reinstall without a local source.
- Keep the server mutation flag and local `allow_install_app` policy fail-closed.

### Observations

- Package names are restricted to a short ASCII charset; no shell interpolation is used.
- APT and Pacman require root or passwordless `sudo -n`; Flatpak and AUR run in the user service context.
- Command timeout and the existing 64 KiB output cap are reused.

### Tests

- Unsafe names and unknown sources are rejected.
- Flatpak uses the expected fixed argument list.
- AppImage reinstall is refused with an actionable message.

## P1.6 — Workspace project configuration

### Scope

- Configure project roots per owner through `workspace_dirs`.
- Collect only `.vscode/settings.json`, `.vscode/tasks.json` and `.vscode/launch.json`.
- Keep workspace files in the `workspace` category with independent hashes.

### Observations

- Project roots are explicit operator configuration; the agent never scans the home directory.
- Symlinks, binary content, secret-bearing content and paths outside the home are skipped.
- Workspace limits are 256 KiB per file and 2 MiB per cycle.

### Tests

- Explicit file discovery, symlink and traversal rejection.
- Agent upload of a configured project file with state persistence.
- Server workspace directory configuration and validation.

### Next

- P1 is complete; choose the next product priority before extending the collection contract.
