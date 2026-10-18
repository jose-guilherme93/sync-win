---
id: sessions/261017-log-scope
type: session
title: "The application log viewer was empty: owner scoping removed"
description: >-
  All 1,514 rows in the dev `logs` table had owner_id = ''. The Settings log
  viewer filtered by owner, so it showed nothing for any account, and the new
  SSE stream inherited the same emptiness. Application logs are now the
  fleet-wide operator view they claim to be; device journal logs stay scoped.
tags: [logs, authorization, bug, sqlite, sse]
source: agent
created: 2026-10-17
updated: 2026-10-17
status: active
---

## How it was found

While verifying the new SSE stream against the real dev stack, the stream
connected and stayed silent. `GET /api/logs` then returned `total=0` for the
demo account. A read-only query against `data-dev/sync-win.db` showed why:

    select coalesce(owner_id,'<null>'), count(*) from logs group by 1;
    ('', 1514)

Every row had an empty owner. The HTTP middleware, startup logs, sync summary,
status watcher and audit calls never set `user_id`, and none of them carry a
`device_id` either, so `COALESCE((SELECT owner_id FROM devices ...), user_id)`
resolved to ''. The owner filter (`owner_id = ?`) therefore matched nothing.

The app-logs session's tests seeded rows with `UserID: owner`, which made the
scoping test pass while the producer wrote no owner at all. This is the
"exercise the producer, not the table" failure AGENTS.md warns about: the
viewer was verified with its own fixture.

## The decision

Application logs are **server-wide**, not per owner. Most entries are written by
the server itself (startup, HTTP, agent, sync), which has no owner to attach, and
the viewer is an Administration screen. Every authenticated account is already
an operator per `SECURITY.md`, and the log settings are already global. An
owner-scoped operator view could only ever be empty.

- `/api/logs`, `/api/logs/{id}`, `/api/logs/stats`, `/api/audit`,
  `/api/errors`, `/api/logs/stream` and `DELETE /api/logs/delete` no longer
  filter or delete by owner. They still require a session.
- The stream's default cursor is the global `MAX(id)`.
- Device journal logs (`/api/devices/{id}/logs`, the `device_logs` table)
  remain owner-scoped through the device ownership check — that is per-device
  data, not an operator log.
- `SECURITY.md` states the consequence: with public registration enabled, the
  instance is trusted-admin-only, the same caveat as the log settings.

`owner_id` stays in the `logs` table. If a future per-owner log ever exists, the
query builder still supports `OwnerID`; the operator endpoints simply do not
use it.

## Verification

- `TestLogsListIsOwnerScoped` became `TestLogsListIsFleetWide`: two accounts now
  each see both seeded entries.
- `go test ./...` green; the SSE tests still pass (their fixture writes and the
  stream reads the same fleet-wide table).
- Confirmed against the dev stack: `GET /api/logs?limit=5` now returns rows
  (1,514 before the change returned zero), and the live stream pushes a new
  entry.
