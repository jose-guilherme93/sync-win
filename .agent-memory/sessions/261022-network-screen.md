---
id: sessions/261022-network-screen
type: session
title: "Network screen: the whole network view, collected and displayed"
description: >-
  The Network screen showed byte counters and a socket table. It now answers
  the three questions an operator asks — is the link healthy, what is using the
  bandwidth and what is reachable from outside — with the container links
  visible, a per-interface traffic history on the server and the kernel's own
  packet health.
tags: [network, telemetry, agent, contract, frontend, charts]
source: agent
created: 2026-10-22
updated: 2026-10-22
status: active
---

## What changed

- **The agent reports the container plumbing instead of dropping it.** The
  interfaces used to be filtered at the source, which hid the bridge that was
  carrying the traffic. Every interface is reported now with `kind`
  (physical, wifi, tunnel, bridge, veth, loopback, virtual) and `counted`. The
  agent sums only the counted ones, so the fleet numbers did not change while
  the operator can finally see the veth pair.
- **A `network_health` block** travels with the telemetry: the TCP counters
  from `/proc/net/snmp` with a per-second retransmission delta, the socket
  table by state and the conntrack usage. It never fails the telemetry cycle:
  a missing `/proc` file leaves a counter at zero.
- **A network topology section** travels with the system inventory (300 s):
  the gateway, the resolvers, the routes, the neighbour table, the radio
  quality and the tunnels. The firewall is reported as the state of its unit,
  never as rules.
- **The server keeps a per-interface history** (`network_history`): one bucket
  per interface per minute, rolled up into five minute buckets after two days
  and pruned after seven. The global telemetry history only has the summed
  rate, which cannot say which link was slow.
- **The screen** has a traffic chart with a 1h/24h/7d window and a
  download/upload direction, the interfaces with their kind, driver, duplex,
  carrier changes and the hardware counters, the IP configuration, the packet
  health, the neighbours, the wireless radios, the tunnels, the firewall and
  an exposure summary. The full socket inventory stayed in Services, which
  already has the search and the drawer for it.
- The rules that grade a link, a signal, a retransmission rate or an exposure
  live in `web/src/lib/network.ts`, so they are tested without a browser.

## What the unprivileged agent can and cannot see

This is the boundary that shaped the whole screen, and it is worth keeping:

| Visible | Not visible |
| --- | --- |
| routing table, gateway, resolvers | firewall rules |
| neighbour table (ARP and NDP) | Wi-Fi SSID, channel and bitrate |
| radio signal, noise and quality | the peers of a tailscale or wireguard tunnel |
| the state of the firewall unit | the process behind another user's socket |

Anything on the right-hand column is absent, never zero. A zero would read as
a fact, and "the firewall is not running" is a very different claim from "the
agent could not read the rules".

## Two things the verification caught

- The kernel lists one route per source address, so the IPv6 default route
  arrived twice. The routes are deduplicated now, and a next hop of `::` or
  `0.0.0.0` is not offered as a gateway.
- The exposure card said "every listening socket is bound to the loopback
  address" on a device with no listening socket at all. The two cases are
  separate now.

## Verification

The real path, not a seeded table: the dev stack plus two Alpine devices
running the real agent, the agent downloading its binary from the dev server.
The topology, the health block, the per-interface history and the screen were
checked against the live agent, in the browser, with Chrome started and killed
inside the same command.
