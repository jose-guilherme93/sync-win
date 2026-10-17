---
id: sessions/261017-alert-acks
type: session
title: "Persistent alert acknowledgements, and a stable alert identity"
description: >-
  Moved alert acknowledgement from component state to the server so an ack
  survives a reload and follows the owner to another browser, and gave each
  insight a stable rule key so acknowledging "CPU 92%" keeps hiding the finding
  when the next sample reads 95%. The rule set stays in lib/insights.ts.
tags: [alerts, ack, sqlite, api, ui, insights]
source: agent
created: 2026-10-17
updated: 2026-10-17
status: active
---

## What was asked

The Alerts screen acknowledged findings only in component state. A reload
brought every alert back, and the acknowledgement did not exist on any other
device. The roadmap listed `GET /api/alerts` + ack as missing.

## What was built

Worktree `sync-win-alert-acks`, branch `feat/alert-acks`.

### The split that was chosen

Alerts are **derived in the dashboard** by `web/src/lib/insights.ts`, which is
also what Home, Reports and the meter colours read. Re-deriving the same rules
in Go would create a second set of thresholds that can silently disagree with
the screen — the exact failure the single-source comment in that file exists to
prevent. So the server stores only the part the client cannot: the
acknowledgements.

- **`server/internal/store/alerts.go`** — `alert_acks (owner_id, alert_id,
  acked_at)` with a composite primary key; `ListAlertAcks`, `AckAlert`,
  `UnackAlert`, `ClearAlertAcks`. `AckAlert` is idempotent and keeps the
  original timestamp. Ids are trimmed, length-capped, UTF-8 validated and
  checked for control characters, because they arrive from the client.
- **`server/internal/app/alerts.go`** — `GET /api/alerts` (list), `POST
  /api/alerts` `{id}` (ack), `DELETE /api/alerts?id=...` (unack one) and
  `DELETE /api/alerts` (restore all). Owner-scoped through `requireSession`;
  cookie clients go through the existing CSRF middleware.
- **Stable rule keys** — `Insight` now carries `key`. CPU/memory/temp/battery/
  agent/kernel use fixed keys; disks and interfaces use
  `disk:<mount>` / `net:<iface>`. The id is `<device id>:<key>`. Before this,
  the id was built from the rendered text, so acking `CPU 92%` came back as
  `CPU 95%` on the next sample.
- **`Alerts.svelte`** — loads acks on mount, acks optimistically and rolls back
  on failure with a visible error, restores all through the API. A dirty guard
  stops the initial GET from overwriting a click made while it was in flight.
- **`App.svelte`** — passes `authHeaders` to the screen.

## Verification

- Server: `go test ./internal/app ./internal/store` green (74s / 68s), `gofmt`
  clean. New handler tests cover the empty list (`[]`, not `null`), the round
  trip, idempotent re-ack, owner scoping, unack/restore, invalid ids, missing
  session and unsupported methods.
- Web: 20 files / **152 tests** pass (was 19 / 145), `svelte-check` 0 errors,
  `vite build` clean. New `Alerts.test.ts` covers rendering, persisting,
  hiding a server ack, restore, and both failure paths (save and load).
- Not exercised in a browser yet; the dev stack has no seeded alert.

## Not done

- **Server-side alert derivation.** `GET /api/alerts` returns acknowledgements,
  not alerts. A future server-side rules engine would have to become the single
  source and feed the meters too, or the screens can disagree.
- **Resolution semantics.** An ack persists until the operator restores it; if
  a condition clears and returns, it stays acknowledged. Detecting resolution
  needs history, which this deliberately did not add.
- The acknowledgement id is client-generated. The server trusts its shape, not
  that the device belongs to the owner; the ack is only ever read back by the
  same owner, so a forged id can only hide a row from the forger.
