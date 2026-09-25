---
id: sessions/260925-p1-functionality
type: session
title: "Completed P1 functionality and agent updates"
description: >-
  Connected preference sync, application inventory, save synchronization,
  policy-gated restore and verified automatic agent updates.
tags: [p1, preferences, applications, saves, restore, updates, agent, session]
source: other
created: 2026-09-25
updated: 2026-09-25
status: active
---

## What happened

- Confirmed P1.1 was already complete and resumed at P1.2.
- Added the application inventory collector for APT, Flatpak, Pacman, AUR and shallow AppImage discovery.
- Connected collection and authenticated upload to the daemon using the embedded 300-second contract interval.
- Prevented failed or partial collection from replacing the last complete server inventory.
- Aligned the server `AppInfo` JSON field with the documented `path` contract.
- Added save discovery, base64 encoding, chunked sync, separate hashes and policy-gated atomic restore.
- Added server encoding persistence, game filtering and restore payload tests.
- Migrated the live desktop agent to a `joseti` user service with correct HOME/state paths.
- Added checksum-verified automatic updates with atomic replacement and rollback.
- Verified a controlled 0.6.1 -> 0.6.2 upgrade, then restored 0.6.1.
- Fixed the device modal reset by initializing the active tab once on mount instead of on every reactive prop update.
- Made empty device file responses JSON arrays instead of `null`, removing the dashboard `invalid files response` error.
- Implemented fixed-command `install_app` for APT, Flatpak, Pacman and AUR with fail-closed policy and privilege handling.
- Added operator-configured workspace project roots and explicit `.vscode` file collection.
- Updated the contract, `ROADMAP.md` and the active P1 functionality context.

## Validation

- `go test ./...` passed for `agent` and `server` using `golang:1.25` in Docker because Go is not installed on the host.
- `go vet ./...` passed for both modules.
- `npm run check` passed in the web development container with 0 errors and 15 pre-existing accessibility warnings.
- `git diff --check` passed after the final P1.3 documentation update.
- Workspace config tests cover explicit discovery, symlink/traversal rejection, server configuration and agent upload persistence.
- Live user service synchronized three real preference files and a binary save test.
- Live user update timer completed successfully; a controlled 0.6.1 -> 0.6.2 upgrade and rollback path was verified.
- Pre-existing untracked `../.agents/`, `../skills-lock.json` and `.opencode/` were not modified.

## Open thread

- P1 and the operational update gate are complete; choose the next product priority before extending the contract.
