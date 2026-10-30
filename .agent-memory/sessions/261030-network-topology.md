---
id: sessions/261030-network-topology
type: session
title: "Network topology: the map of every network a device belongs to"
description: >-
  The Network screen showed the counters, the interfaces and the neighbour
  table. The new Topology screen turns that snapshot into a map: the device in
  the centre, one hub per LAN segment and overlay, and every node the device can
  see. Tailscale peers arrive from the daemon's world-readable local API socket,
  so the tailnet is part of the map without root and without the tailscale
  binary.
tags: [network, topology, tailscale, agent, contract, frontend, graph]
source: agent
created: 2026-10-30
updated: 2026-10-30
status: active
---

## What changed

- **A new device screen, `Topology`**, sits in the Hardware group next to
  Network, and the Network screen links to it. The device is the centre, each
  LAN segment and overlay is a hub, and the members orbit their hub. Pan, zoom,
  drag-to-pin, reset, network focus, and a detail drawer for every node. A
  narrow canvas stacks the hubs above and below the device instead of squeezing
  two cards side by side.
- **The agent reads Tailscale.** `agent/collectors/network_overlays.go` does a
  `GET` on `/localapi/v0/status` over `/var/run/tailscale/tailscaled.sock`
  (0666 on a default install). It keeps the tailnet name, the self node, and
  every peer's hostname, OS, overlay addresses, online state, direct/relayed
  path, relay, tags, exit-node flags and last seen. No key, no user account and
  no `tailscale` binary. Peers are sorted online-first before the contract cap,
  so a large tailnet never loses the reachable nodes.
- **The topology payload gained `overlays[]`.** The server stores it in the same
  `network_json` blob, so no migration and no endpoint change; an older server
  ignores the field and an older agent omits it.
- **The LAN view is passive and honest.** Neighbours are grouped by interface,
  deduplicated (the gateway is drawn once as the way out), sorted reachable
  first, and matched to the fleet by address so another SyncWin device is
  recognisable. Container bridges (`docker0`, `br-*`, `veth*`) are excluded:
  their neighbours are containers, not a network the host joins. A neighbour on
  an interface the hardware snapshot does not describe still appears, with the
  segment address unknown.
- **WireGuard is named, not hidden.** A tunnel whose peers need root is listed
  under "Tunnels without a peer list" instead of disappearing. A Tailscale
  socket that cannot be read is reported as an error on the overlay, never as an
  empty peer list.
- **Layout and rules are pure and tested** in `web/src/lib/topology.ts`: graph
  construction, fleet recognition, `ipOnly` / `ipv4Network`, and a deterministic
  force layout (seeded initial positions, fixed self and hubs, collision sized
  for the label, stacked hubs below 560px). The screen only draws the result.

## The boundary worth keeping

| Visible | Not visible |
| --- | --- |
| LAN neighbours via ARP/NDP | devices this host never talked to |
| gateway, default route, DNS | firewall rules |
| Tailscale peers via its local API | WireGuard/ZeroTier peers without root |
| direct/relayed path, relay, tags | Tailscale user accounts and keys |

The agent does **not** ping-sweep or port-scan to fill the LAN. The screen says
so under "What is not here": a neighbour is a device this host exchanged traffic
with recently, not the whole segment.

## Verification

- The real path, not a fixture: the dev stack from this worktree plus the host's
  own agent binary, enrolled as `guilherme-topology`. It reported 21 neighbours
  and a 17-peer tailnet (6 online) with no overlay error; the API snapshot and
  the browser both showed the same map.
- Browser: the map, the network strip, a peer drawer (path, addresses, tags) and
  the Tailscale network drawer with its member list, at 1440 and at 390. The
  Network screen's "Open the network map" button navigates to the section.
- Agent `go test ./...`, server `go test ./...`, web 322 tests,
  `svelte-check` 0 errors, `vite build` clean. `go test -race` could not run
  here because the local toolchain has CGO disabled.
- The first map draft drew five docker bridges and buried the real segment; the
  fallback now skips the plumbing prefixes covered by the agent. That was a
  finding from looking at the rendered map, not from a unit test.

## Follow-ups

- The Tailscale local API is 0666 on a default install. A host that hardened the
  socket permission shows an overlay error with the reason; a documented
  `tailscale` group hint belongs in the install docs if that becomes common.
- The map is a snapshot on the 300 s inventory cycle; it is not a live scan by
  design. A forced refresh would need a new device command and is not part of
  this change.
