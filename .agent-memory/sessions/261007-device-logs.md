---
id: sessions/261007-device-logs
type: session
title: "Making the device Logs screen work"
description: >-
  The Logs screen existed and always showed nothing. The server was erasing the
  log batch on five of every six telemetry cycles; lines now live in their own
  table with a viewer that filters, searches and pages.
tags: [logs, journal, telemetry, store, dashboard, session]
source: agent
created: 2026-10-07
updated: 2026-10-07
status: active
---

## What happened

Worktree `/home/joseti/projects/sync-win-device-logs`, branch
`fix/device-logs-screen`, because the shared checkout had another agent's
uncommitted `scripts/e2e.sh`.

Three commits:

```
7b36115 fix(server): persist device log history instead of losing it each cycle
6c77a96 feat(web): device log viewer with filtering, search and paging
cf30124 fix(agent): report journal read failures and strip the source colon
```

## The diagnosis that mattered

The screen read `hardware_json`. That column is rewritten wholesale by every
telemetry post, and the agent samples the journal on one cycle in six, so
`omitempty` dropped the field on the other five and the next write erased the
previous batch. Verified on the live dev database: `hp240-debian` had a 5.7 KB
`hardware_json` with zero log entries.

The symptom was "the screen shows no logs". The cause was "the server deletes
the data between polls". Those look nothing alike from the UI, which is why the
empty state said the agent had not reported anything — which was true, and
useless.

Collection was never broken. `journalctl` works fine as the unprivileged
`sync-win` user.

## Three things worth remembering

**Two bugs were mine, caught by tooling, not review.** `if s := query.Get("since")`
shadowed the `*Server` receiver with a string and broke `s.writeError`. And
`normalizeDeviceLogLevel("")` returned `"info"`, so an unset level filter folded
onto `level = 'info'` and hid every other severity — that one surfaced as four
test failures at once, because an unfiltered query returned 10 of 30 rows.

**The `/api/devices` 500 that blocked viewing anything was my seed script.**
It wrote `NULL` into nullable columns that `scanDevice` scans into plain
`string`. Not a regression, but it exposed a real latent fragility that is
recorded as a known issue in the decision file.

**The failing web test was correct to fail.** `DeviceModal.test.ts` asserted the
logs tab fetched `/detail`. That fetch is gone by design. The test was rewritten,
not the component — it still guards the original invariant (exactly one request,
no runaway reactive refetch).

## Verification

- `go vet` clean, `go test ./...` green on server and agent.
- Web: `svelte-check` 0 errors, 120 tests / 17 files green, `npm run build` OK.
- API hand-checked against 600 seeded rows: every filter, paging, the 400 on a
  bad `since`, both 404 paths, and `%` matching literally rather than as a
  wildcard.

## Not done

`app`-level tests for the new endpoint — verified by hand only. And nobody has
looked at the screen: 120 passing tests say nothing about whether the layout
reads well.

## Environment note

This host has no Go toolchain. All Go verification ran through a Docker wrapper
around the project's pinned `golang:1.25-alpine`, kept outside the repo. The
preview stack ran as a separate compose project on 8089/5174 with a copy of
`data-dev`, leaving the shared checkout's stack on 8088/5173 untouched.