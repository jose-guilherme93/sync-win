---
id: sessions/261018-docker-screen
type: session
title: "Docker screen: live state, honest commands, and a design that matches the panel"
description: >-
  The Docker screen read a cache sampled once a minute, polled results once a
  second, and spoke its own hardcoded colour language. It now asks the device
  for the list, streams state, polls actions on a two-second agent cadence, and
  uses the shared panel components. Along the way the container ports were found
  to have never been decoded from the Docker Engine at all.
tags: [docker, ui, polling, sse, agent, bug]
source: agent
created: 2026-10-18
updated: 2026-10-18
status: active
---

## What was reported

The Docker screen was slow to show container information and its interface did
not match the rest of the dashboard.

## Root causes

1. **The list was always one sample behind.** `GET /api/devices/{id}/docker`
   returns the cached container list, and the agent samples that list only on
   its slow (~60s) cycle. The old screen's Refresh button re-read the same
   cache, so a new device or a just-started container could look empty or stale
   for up to a minute. Nothing ever asked the agent for a list.
2. **Actions waited for the telemetry cycle.** `processDockerRequests` ran
   inside the main telemetry loop (10s by default), so a queued start/stop/list
   could wait a full interval, and longer behind a slow inventory step.
3. **Result polling was slow and silent.** The UI polled once a second for up
   to 20–30 attempts and ignored 202 responses, so a slow device produced a
   spinner with no explanation.
4. **The cache was not refreshed by a list result.** A completed `list` was
   handed to the requester and forgotten, so the next SSE broadcast went back
   to the stale snapshot.
5. **Ports had never worked.** `dockerAPIContainer.Ports` used
   `[]DockerPort`, whose JSON tags are `private_port`/`public_port`, but the
   Engine spells them `PrivatePort`/`PublicPort`. Go's case-insensitive matching
   does not bridge an underscore, so every port decoded as zero and the screen
   showed "no ports" for containers that publish ports.
6. **No compose list meant "null".** `DockerComposeFiles()` can return a nil
   slice, which marshals to `null`; the old UI's JSON array expectation treated
   that as an error.
7. **The interface was legacy.** `DockerTab.svelte` was a 1,500-line component
   with hardcoded slate/blue colours, its own confirmation dialogs, and
   compose/prune hidden behind container tabs even with nothing selected.

## What was built

- **Agent**: a dedicated `runDockerRequestPoller` drains the server queue every
  two seconds, independent of telemetry, with exponential backoff on errors. A
  device without Docker now answers queued commands with "docker not available"
  instead of leaving them pending. Compose read always marshals `[]`, never
  `null`. The Engine port shape got its own `dockerAPIPort` type and a
  `containersFromAPI` conversion with a regression test.
- **Server**: the queue remembers each request's type, so a completed `list`
  can replace `docker_state_json` before the result is consumed and before the
  SSE broadcast. Container `created_at` is carried through the store and the
  state payload. Tests cover list refresh, non-list results not clobbering the
  cache, unreadable lists keeping the last good snapshot, and type retention.
- **Dashboard**: `DockerTab.svelte` was rewritten around the shared tokens:
  tiles, a searchable card with state buckets, a right-hand drawer for
  overview/resources/logs/shell, and visible compose and cleanup cards. SSE
  drives live state; Refresh issues a real `list` command; results use adaptive
  polling with a stated timeout. `lib/docker.ts` normalises both wire shapes,
  deduplicates IPv4/IPv6 port bindings, and parses results; `lib/docker-client.ts`
  owns queue/result/stream plumbing. Both have unit tests.

## Verification

- `go test ./...` server and agent, plus `-race` in `golang:1.27`.
- `npm run test` **205 tests**, `npm run check` 0 errors, `vite build` clean.
- `golangci-lint --new-from-rev=develop` (server and agent): 0 issues.
- `WEB_PORT=5175 DEV_HTTP_PORT=8099 scripts/e2e.sh`: **68/68** checks, including
  every device section.
- A real dev stack with a privileged Ubuntu test agent on the host Docker socket:
  list showed the 23 host containers with ports (`8099 → 8080/tcp`), stats and
  logs/exec worked, a disposable container was started/stopped/removed from the
  UI in ~3s each, compose discovered a file and opened its editor, and the prune
  dialog produced the right consequence text.

## Follow-ups worth knowing

- The agent still polls the queue every two seconds while idle. That is a
  deliberate trade: the endpoint is in-memory and the screen's responsiveness
  was the complaint. If idle request volume ever matters, make the cadence
  adaptive rather than slow the first list.
- Paused containers cannot be started or restarted by Docker; the UI says so
  instead of offering a button that fails. Unpause is not in the agent's request
  type list.

## Follow-up: inline panel instead of a drawer

The first version put details in a 470 px right-hand drawer. On review it was
rejected: the logs were too narrow to read, and the details opened away from the
row that was clicked. The revised screen was specified through a grilling round
and agreed:

- Clicking a container expands a full-width panel **below its row**, one at a
  time. The row scrolls to the top so the panel is visible immediately.
- The panel always shows CPU, memory, network (derived rates), disk I/O
  (derived rates) and PIDs. Tabs underneath are `Info | Logs | Terminal`.
- The screen itself has top-level tabs `Containers | Compose | Cleanup`, so the
  maintenance cards no longer compete for vertical space with the list.
- Stats use one self-scheduling request at a time (500 ms pause after a result),
  and stop when the panel collapses or the tab is hidden. No per-row metrics.
- Logs poll a tail every 3 s while the tab is open, with wrap, timestamp toggle,
  text filter, line-count selector and copy. A record without a trailing newline
  is split at the next RFC3339 timestamp.
- `inspect` was added for networks, health, restart policy, entrypoint/command,
  mounts and labels. Env is never returned; credential-looking labels and
  command arguments are masked. `pause`/`unpause` were added, and `kill` is in
  the panel with confirmation.
- The Docker package has no new dependencies; sparklines stay on the existing
  canvas primitive.

Verification on the real dev stack: inspect over the new agent, pause/resume on
a disposable container, kill/remove, exec, live metrics and full-width logs.
`npm run test` 215 passed, `svelte-check` 0 errors, build clean, `go test ./...`
and golangci-lint clean, e2e 68/68.

The dev stack still runs with `SYNCWIN_ENABLE_DOCKER_MUTATIONS=false`, so
mutations answer a clear 403 until that flag is enabled; reads, metrics, logs
and inspect work regardless.

## Second revision: large side panel

The inline panel was then rejected in favour of a large panel sliding in from
the right. Content is unchanged; only its location moved.

- The panel is `min(980px, 96vw)` wide, fixed, full height, with a dimmed
  backdrop and a 220 ms slide-in. Escape, the backdrop and the X all close it.
- The container name was added to the panel header; the inline row no longer
  provides it next to the content.
- The header is sticky so actions and close stay reachable while logs scroll.
- The panel keeps the same tabs, metrics loop, logs polling and action set.

## Third revision: tabbed side panel

The side panel kept all information stacked in one long Info tab. It was
reorganised into five focused tabs so each one fits a smaller viewport without
losing content:

- `Overview`: status, created, image, id, health/flags and published ports.
- `Stats`: CPU, memory, network and disk cards with larger sparklines and PIDs.
  Metrics now poll only while this tab is open; the previous sample window is
  reset if the tab was closed for more than ten seconds.
- `Logs`: unchanged full-width viewer with wrap, timestamps, filter, tail,
  follow and copy.
- `Terminal`: unchanged one-shot exec.
- `Config`: platform, restart policy, limits, user/workdir, command, mounts,
  networks and labels. Labels are collapsed by default and exclude
  credential-looking keys.

The header keeps only the name, state, id and actions; the panel is still
`min(980px, 96vw)`, slides from the right and closes with Escape, the backdrop
or the X.

No commit was made for this revision yet, per the user's instruction: commits
only with permission, and only to the local `develop`.
