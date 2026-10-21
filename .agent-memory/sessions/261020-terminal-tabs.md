---
id: sessions/261020-terminal-tabs
type: session
title: "Remote access: one SSH session per tab, capped at the documented three"
description: >-
  The SSH terminal card held a single xterm instance and a single WebSocket, so
  a second session replaced the first. It now keeps a tab per session, each with
  its own account, ticket, socket and renderer, and closing a tab ends only that
  session.
tags: [remote-access, terminal, xterm, tabs, frontend, tdd]
source: agent
created: 2026-10-20
updated: 2026-10-20
status: active
---

## What changed

- `lib/terminal-tabs.ts` owns the tab list: the three-session cap, neighbour
  selection when the active tab closes, and stable per-tab labels
  (`alice`, `alice (2)`). A label is fixed at creation so closing a sibling does
  not rename a running session.
- `RemoteAccess.svelte` keeps a `Map<tabId, LiveSession>` of renderers and
  sockets. Each pane is mounted through a Svelte action, so the keyed block owns
  mount and cleanup. Inactive panes are `display:none`; the active one is fitted
  after `tick()`.
- Opening a terminal adds its tab immediately, then requests a ticket with the
  account chosen at that moment. A tab closed while the ticket is in flight is
  detected after the await and never opens a socket.
- Failures are per tab: a refused ticket or a socket error marks only that tab,
  and the alert follows the active tab. Disconnect keeps the tab and its
  scrollback; closing the tab disconnects it.
- The cap is three because that is the documented per-device limit in
  `.agent-memory/decisions/remote-access.md`. The tunnel hub itself does not
  enforce it, so this is a UI ceiling, not a server guarantee.

## Verification

- `web`: 25 files / 239 tests, including 10 new pure tab tests and 21
  RemoteAccess tests (two tabs with separate tickets and sockets, out-of-order
  ticket resolution, switching without reconnecting, output and keystrokes
  routed to the right pane, closing one tab only, the cap, per-tab failures,
  close-during-ticket race, last-tab empty state, account captured at creation).
- `svelte-check` 0 errors (same 6 pre-existing warnings as `develop`), `vite
  build` clean.
- `scripts/e2e.sh` now asserts the tab strip, a refused session reported inside
  its tab, and the return to the empty state after closing; **73/73** checks
  passed against a worktree dev stack (`WEB_PORT=5273 DEV_HTTP_PORT=8188`).
- Real browser: three tabs labelled `default`, `default (2)`, `default (3)`,
  `3 of 3 open`, the "Close a tab to open another" hint, and switching moved
  the single visible panel. The live SSH path still needs a real device with
  sshd, which this session did not have.

## Notes

- `agent-browser` was missing on this machine; it was installed globally and
  `agent-browser install` downloaded Chrome 156. The run needs
  `--args "--no-sandbox"` (the e2e script already passes it).
- A named agent-browser session keeps its launch flags in the daemon; after a
  first launch without `--no-sandbox`, `close` is not enough. Kill the daemon or
  use a fresh session before retrying with the flag.
