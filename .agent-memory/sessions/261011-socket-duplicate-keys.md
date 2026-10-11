---
id: sessions/261011-socket-duplicate-keys
type: session
title: "A duplicated socket blanked Services, Network and Security"
description: >-
  Found the real cause of the Services screen never leaving "Loading…" and of
  the Security screen losing its exposure list and Recommended actions: the
  agent reported one UDP socket twice, and Svelte aborts a component's render on
  a duplicate key in a keyed {#each}. Fixed at the producer and guarded in the
  dashboard, with the three affected screens verified in a real browser.
tags: [dashboard, services, security, network, svelte, keyed-each, collector, sockets]
source: agent
created: 2026-10-11
updated: 2026-10-11
status: active
---

## The symptom and the wrong theory

On `cachyos-x99` the Services screen sat on "Loading system inventory…" forever.
The requests returned 200 with 154 services and 23 ports, and the state was
correct: an instrumented build logged `services=154 ports=23 loaded=true` while
the DOM still showed the loading placeholder. A synchronous write to the same
state inside `onMount` also failed to repaint, so it was never about the network,
a slow response, `try/finally`, or background-browser throttling — the three
theories this work started with.

## The cause

`ss -tulpnH` lists a UDP socket once per bound address, and
`splitSocketAddress` folds `0.0.0.0`, `*` and an empty host onto the same value,
so one socket was emitted as two byte-identical `OpenPort` records. The real data
carried `udp--5353` twice and `udp-::-5353` twice.

Svelte throws `each_key_duplicate` when a keyed `{#each}` has fewer distinct keys
than rows (`length > keys.size` in `internal/client/dom/blocks/each.js`). That
does not skip a row: it aborts the component's render and leaves the previously
rendered DOM in place, so the screen never recovers. Three screens render these
sockets in a keyed list and all three went blank on the same device:

- Services — the whole screen stayed on its loading placeholder.
- Security — the exposure list (public listeners + failed units) and
  "Recommended actions" were missing, while the audit history rendered because it
  had loaded in an earlier flush.
- Network — the listening-sockets table.

This is why the symptom looked intermittent and why it was "not reproduced" on
another device: `ubuntu-vps` reports no duplicate sockets, `cachyos-x99` does.
It is data-dependent, not timing-dependent.

## The fix

Three commits on `feat/ui-polish`:

1. `fix(agent)` — `dedupeOpenPorts` drops records that compare equal. The record
   is its own identity; a differing `Process` is a different socket and is kept,
   so `SO_REUSEPORT` across two processes survives. Two tests, including the real
   `ss` output that caused it.
2. `fix(web)` — `lib/ports.ts` defines `portIdentity` and `uniquePorts` once, and
   the screens both de-duplicate and key on it. A per-screen key that disagrees
   with the guard is exactly how this recurred: the guard is what makes the key
   safe. `SecurityPosture.publicPorts` now carries the full record, because
   trimming it to `{port, protocol}` collapsed the distinction the key needs.
3. `refactor(web)` — `computedStatus` had three byte-identical copies; the shared
   one in `lib/types` wins. `RemoteAccess` and the Security tab were comparing
   against the raw `device.status`, which lags the timestamp by a poll interval.

The agent fix alone is not enough: a device running an older agent keeps sending
the repeats, so the dashboard guard is what makes the deployed fleet work.

## Verification

Real browser, not headless, against a second dev stack (server 8099, Vite 5188)
with its own database snapshot, because the default `agent-browser` session was
shared with another agent's app and silently switching tabs produced two
misleading results.

- Services: `stuck: false`, tiles 59/1/94, **21** port rows (23 − 2 duplicates),
  154 service rows.
- Security: `hasExposure`, `hasPublicListeners`, `hasFailedUnits` and
  `hasRecommended` all true.
- Network: 21 rows, with `*` and `::` correctly distinguished.
- The regression test was proven to fail against the pre-fix code
  (`each_key_duplicate` → blank screen) and pass after it.

`web`: 132 tests, `svelte-check` 0 errors, `vite build` clean.
`agent`: `go build ./...` and `go test ./...` ok.

## Correction to earlier notes

`261009-dashboard-screens.md` records that async values "did not repaint" and
that the `{#key}` workaround over `$:`-derived scalars was the fix. That workaround
was treating a real symptom, but the durable lesson is different and stronger:
when a screen looks frozen or half-rendered, check for a thrown
`each_key_duplicate` first. It presents as a rendering failure with no visible
error, and it is caused by data, not by timing.

## Still open

- The Security exposure tags render `22/tcp` twice for the IPv4 and IPv6
  wildcards. They are genuinely two sockets and the counts stay consistent with
  the Services screen, but the tag cannot show which is which. Left alone
  deliberately: changing it would desync the two counts.
- Logs still has its own time-range control (`lib/device-logs.ts`) separate from
  the topbar's chart range (`lib/timerange.ts`), with a different option set. Two
  sources of truth for one concept; reconciling them is a larger change.

## Follow-on work in the same session

Packages and Logs were then taken through the same visual pass.

- `fix(agent)`: apt packages had **no version at all** — `apt-mark showmanual`
  prints names only — so the dashboard's version column was empty on every
  Debian-family device. Versions are joined from `dpkg-query`, best-effort, so a
  machine without dpkg still reports its list. Verified with the real agent:
  92 of 92 packages carried a version, where the previous build reported 0.
- `fix(web)`: Packages gained the headline row the other screens open with, and
  name/version became two real columns. An absent version renders as `—` and the
  headline counts how many have none, because "not reported" and "the column is
  broken" otherwise look identical.
- `fix(web)`: Logs rows were inheriting global `.level-*` rules from `app.css`
  (defined for the server-log view in `App.svelte`), which is why every row had a
  coloured background — blue-tinted info rows in a single-accent design. The rows
  use `row-*` now. The level counts also moved from the filter chips into a
  headline row, and the footer stopped restating the total.

A note on method: the `--crit-dim` row fill and the duplicated footer text were
the two things that *looked* like the problem. The actual cause of "fora do
design" was a class-name collision with a legacy global stylesheet, found by
reading the computed background off a row in the browser rather than by reading
the component's own CSS.

