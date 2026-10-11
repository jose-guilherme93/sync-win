---
id: sessions/261009-dashboard-screens
type: session
title: "Device screens rework: network, storage, services, security, docker"
description: >-
  Split the network, storage, containers and security sections out of the shared
  overview and the device modal into dedicated screens, reworked services into a
  services/ports split with a detail drawer, made stored Lynis reports readable,
  enriched the network collector with link metadata, and fixed the collapsed
  sidebar rail.
tags: [dashboard, network, storage, services, security, docker, lynis, collector]
source: agent
created: 2026-10-09
updated: 2026-10-09
status: active
---

## What happened

Worktree `sync-win-ui`, branch `feat/dashboard-rework`, based on
`feat/remote-access` @ `a708a80`. A parallel agent was editing
`sync-win-remote-access` at the time, so this work was isolated in its own
worktree and a second dev stack (server on 8188, Vite on 5174) ran against a
copy of the dev database.

Three commits:

1. `fix(web)`: collapsed sidebar rail — hide group captions, centre icons,
   hairline dividers, single centred toggle.
2. `fix(server)`: the command-result cap was 64 KiB while the agent contract
   allows a 1 MiB Lynis report (`report_max_bytes`). Real reports were dropped
   before `SaveSecurityAudit`, so the dashboard showed "No security audits yet"
   and the command stayed `queued`. Cap raised to 1 MiB.
3. `feat(web)` + collector: dedicated Network, Storage, Services, Security and
   Docker screens; agent network interfaces now carry `mac`, `mtu`,
   `oper_state`, `speed_mbps`, `addresses`, `rx_dropped`, `tx_dropped`.

## Verified

- Real Lynis audit end-to-end against the running dev stack: queued → agent ran
  `lynis audit system` → report saved (hardening index 61) → readable in the
  Security screen (warnings/suggestions shown).
- Network screen renders live per-interface metadata and rates from the
  enriched collector (`eth0` MAC/MTU/addresses/up, `tailscale0` addresses).
- `web`: 120 Vitest tests pass, `svelte-check` 0 errors, `vite build` clean.
- `agent` `go test ./...` and `server/internal/store` tests pass.

## Known gaps (not done here)

- SMART, inode usage and per-disk I/O are still not collected; the Storage
  screen labels the gap instead of faking rows.
- Network connections/sockets (established TCP, routes, DNS) are not collected;
  the Network screen shows the listening sockets the agent already reports.
- Docker volume/image/network listing does not exist in the agent yet.

## Svelte note worth remembering

Several device sections did not repaint when a value arrived from an async
fetch in the shell or the screen; the UI only caught up on the next poll tick.
For the Security posture this was worked around by storing the value on the
tracked device-detail object and keying the affected chip block with
`{#key}` over `$:`-derived scalars. If a similar "value arrives but the DOM
stays stale" symptom appears, that pattern is the fix, not a rewrite.
