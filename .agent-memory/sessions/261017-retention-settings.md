---
id: sessions/261017-retention-settings
type: session
title: "Telemetry retention moved into Administration → Settings"
description: >-
  The telemetry retention controls were the last observability setting hidden in
  the user menu's Data Retention modal. They now live in the Settings screen
  next to the application log settings, with the per-account scope stated and
  the same validation as the API.
tags: [ui, retention, telemetry, settings, storage]
source: agent
created: 2026-10-17
updated: 2026-10-17
status: active
---

## What was asked

Data retention should be visible and editable now. The application log screen
already had its own retention; the telemetry history retention was still a modal
opened from the user dropdown.

## What was built

Worktree `sync-win-retention`, branch `feat/retention-settings`.

- **`AppSettings.svelte`** gained a "Telemetry retention" card below the log
  settings: raw hours (10 s samples), 1-minute days, 5-minute days and 1-hour
  days, each with its range and what it controls. Loads from `GET /api/retention`
  on mount, saves through `PUT /api/retention`, and shows either the confirmation
  or the server message on failure.
- The card says **this account's devices** because telemetry retention is
  per-owner in the store, unlike the application log settings above it, which
  are global. In a single-operator deployment they are effectively the same, but
  the distinction is real if a second account ever exists.
- **`App.svelte`** lost the `Data Retention` menu item, its five state
  variables, the load/save functions and the modal markup.
- **`app.css`** lost the `.retention-*` block (1200 bytes). Nothing else used it,
  and the Settings screen has its own scoped styles.
- The screen title changed from "Application logs" to "Settings", matching the
  sidebar label now that it holds both concerns.

## Verification

- New component tests: stored retention values render, changing raw and 1-minute
  values sends the exact PUT body, a successful save shows the confirmation, and
  a 500 surfaces the server error in a `role="alert"` instead of pretending it
  was saved.
- `npm run test` (20 files / 155 tests), `svelte-check` 0 errors, `vite build`
  clean. `grep` confirms no remaining `retentionOpen` or `.retention-*` usages.
- Live dev stack: `/api/retention` returns 2/7/30/365 for the demo account and the
  Settings screen renders those values.

## Follow-up (same day)

Live verification exposed a wire mismatch: `GET /api/retention` returned
`"1m_days"`, `"5m_days"` and `"1h_days"` while `API.md` and both the old modal
and the new card use `"resolution_1m_days"`, etc. The store struct's JSON tags
were the odd ones out, so every save decoded zeros and failed validation — the
old modal's Save had never worked. The tags now match the SQL columns and the
documented API, and invalid values return `400` instead of `500`
(`store.ErrInvalidRetention`). `retention_test.go` pins the exact GET defaults,
the PUT body the card sends, the reread, the out-of-range 400s and the session
requirement.

## Not done

- Telemetry retention is still per owner. Making it global would match the log
  settings, but would also make one account's choice apply to every owner's
  devices; that is a product decision, not a bug.
- The retention cleanup cycle itself is unchanged (it already reads these
  settings).
