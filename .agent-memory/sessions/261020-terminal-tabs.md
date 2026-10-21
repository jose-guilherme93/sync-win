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

## Follow-up: six tabs, a `+` control and program titles

The tab strip now always renders, with a `+` button (`aria-label="New terminal
tab"`) at its end; the green "New terminal" button is gone and Disconnect is a
neutral ghost button. `MAX_TERMINAL_TABS` and the agent contract's
`max_sessions` both moved from 3 to 6. Account labels are no longer numbered:
the tab shows the program's OSC window title (`onTitleChange`) and falls back to
the account. The strip is visible with zero tabs, so the first session starts
from `+` too.

Verified with six live sessions on `ubuntu-full-1`: all six connected, the strip
scrolled to the active tab with `+` still pinned in the card, and a foreground
command that set the title to `btop` renamed its tab. Unit tests: 9 tab-list and
22 RemoteAccess tests (243 web total); e2e 74/74, including the add-tab control.
A session the device refuses before attaching now reports why in its tab instead
of showing a bare `[disconnected]`.

## Follow-up: compact terminal chrome

The full-width account picker and the explanatory paragraph are gone. The card
header holds the session count, a 9.5rem account select and a ghost Disconnect;
the tab strip sits directly under it, with `+` immediately after the last tab
and the active tab scrolled into view. The `Connecting…` line written into the
terminal was removed — status lives in the tab dot and the header count, so no
stale line survives a successful connection.

## Follow-up: height follows the available space

The terminal no longer guesses with `100dvh - N`: that guess ignored the topbar,
screen header, alerts and tab strip and left the pane below the fold. On mount,
resize and tab changes the screen measures the active pane's position inside
`main.content` and sets the height to the remaining space (minimum 240px).
Scrolling does not change it because the offset is taken inside the scroll
content. Measured 640px at 879×854 and 363px at 1280×577, both ending above the
scroll area's bottom.

## Follow-up: screen organized around the terminal

The audit list moved out of the scrolling page into a fixed side panel. The
layout reserves 272px on the right and absolutely positions the aside to the
row, because a long audit list as a normal grid item stretched the row (the
terminal card reached ~2,000px tall). The terminal card owns the height and the
side panel fills it; the list scrolls internally. Below 760px the panel stacks
under the terminal with a 220px cap.

The fixed device actions (restart agent, package updates, reboot) and their
result log were removed from Remote access and extracted into
`components/device/DeviceActions.svelte`, rendered by Device Settings →
Maintenance. The old cards stretched the page and the actions are gated by
server and device policy, which the Settings note states. Tests moved with the
component; the e2e now asserts the actions in Settings.

## Follow-up: global dark scrollbars

`lib/theme.css` now declares `color-scheme: dark` and one scrollbar treatment
for every element: thin, transparent track, a 10px rounded thumb tinted
`--scrollbar-thumb`, brighter on hover, with the Firefox `scrollbar-color` path
alongside the WebKit pseudo-elements. The xterm terminal receives matching
`scrollbarSlider*` theme colours because it draws its own slider.

Checked in the browser: computed pseudo styles report a 10px bar and
`rgb(43, 43, 49)` thumb; headless Chrome uses overlay scrollbars, so the bar is
not visible in screenshots even though the rule is applied.

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
