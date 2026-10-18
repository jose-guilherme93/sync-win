---
id: sessions/261015-app-logs
type: session
title: "Application logs in a fleet-wide Settings screen, with configurable level and retention"
description: >-
  Added an Administration → Settings screen that surfaces the structured
  application logs already persisted to SQLite, with level/category/device/time
  filters and a live tail, plus application-wide controls for the persistence
  level, console level and log retention. Made the logger's thresholds mutable
  at runtime so a change applies without a restart.
tags: [observability, logs, settings, logger, retention, ui, api]
source: agent
created: 2026-10-15
updated: 2026-10-15
status: active
---

## What was asked

The application already had a full logging backend (structured logger, SQLite
`logs` table, `/api/logs`, `/api/audit`, `/api/errors`) and a "System Logs"
modal reachable from the user menu, but no place in Settings and no configurable
thresholds. The request: observability of everything, and a way to see the
application logs from the configuration.

## What was built

Worktree `sync-win-app-logs`, branch `feat/app-logs`, based on `develop`.

### Backend

- `logging.Logger` level and console level are now `atomic.Int32` with
  `Level`/`SetLevel`/`ConsoleLevel`/`SetConsoleLevel` accessors. They were read
  straight from the immutable `Config` before, which could not change without a
  restart. Atomics because the HTTP handler writes them while request goroutines
  read them on every log call.
- `logging.Settings` (`level`, `console_level`, `retention_days`) with
  `Validate`, plus `ParseLevelStrict` — unlike `ParseLevel` it never folds an
  unknown value onto INFO, so a typo is a 400 rather than a silent reset.
  `AUDIT` and `FATAL` are not accepted as thresholds.
- New single-row `log_settings` table (created in `store.initSchema`, like
  `logs`), with `logging.Store.GetSettings`/`SaveSettings`.
- `GET`/`PUT /api/log-settings`. The GET also returns the settable levels and
  the category list so the frontend pickers cannot drift from the backend.
- Startup loads the persisted settings and applies them; the row is seeded on
  first boot so the screen shows the values actually in effect. The stored level
  wins over `LOG_LEVEL`, so the screen and the logger never disagree. The daily
  cleanup goroutine now reads the cached retention instead of the boot config.

The settings are **global, not per owner**: there is one logger and one `logs`
table, so a per-owner level would apply to every account while appearing scoped.

### Frontend

- New fleet-wide section `configuration` => screen `AppSettings.svelte`, in a
  new sidebar group "Administration", labelled "Settings". It is not
  device-scoped, so `isDeviceSection` in `App.svelte` excludes it.
- The screen: stats (total, errors in 24h, by level), filters (level, category,
  device, time window, debounced search), a listing with expandable metadata, a
  live toggle (5s poll) and a log settings card.
- The `<small>` hints sit outside the `<label>` so the control's accessible name
  stays exact — with them inside, `getByLabelText('Persistence level')` failed
  because the hint text became part of the name.

## Verification

- Server: `go build ./...`, `go vet ./...`, `go test ./...` all green.
  New tests cover the settings endpoints (defaults/effective values, apply +
  persist, validation, session required, method not allowed), the settings store
  round trip, `ParseLevelStrict`, and the runtime level change persisting only
  after `SetLevel`.
- `golangci-lint v2.14.0` (the CI version) on the touched packages: the only
  remaining findings are pre-existing (errcheck/noctx/staticcheck/unused in
  untouched lines). The two introduced were fixed: `QueryRow`/`Exec` to their
  `Context` variants, and a US spelling in a comment.
- Web: 19 files / 144 tests pass (was 18 / 138), `svelte-check` 0 errors,
  `vite build` clean. New `AppSettings.test.ts` covers rendering, the stats
  summary, filtering, persisted values, saving, and a failed request with retry.
- `gofmt -l` clean; agent tests green.
- **End to end, against the real dev stack** (`make dev-d`, real containers and
  SQLite): `GET /api/log-settings` returned the seeded defaults (level DEBUG and
  console INFO from the dev env, retention 14); `PUT` applied DEBUG/INFO/21;
  an invalid `AUDIT` level returned 400; and after `docker compose restart
  server` the GET still returned the stored values, proving the choice is
  persisted and re-applied on boot, not just held in memory.
- **Browser suite: `scripts/e2e.sh` — all 59 checks passed**, including every
  device section and the fleet view, so the new sidebar group did not disturb
  the existing navigation.

## Not done

- `go test -race` could not run locally (no cgo/gcc); CI runs it. The changed
  shared state is `atomic.Int32`, so it is race-free by construction.
- Log streaming is polling, not SSE; the notification SSE plumbing exists but a
  logs stream was left out to keep the change reviewable. **Superseded on
  2026-10-17 by `sessions/261017-log-sse.md`.**
- The "Data Retention" modal (telemetry retention) still lives in the user menu;
  it is a separate concern from application logs and was not moved into Settings.

## Follow-up pass (same day)

Reviewed the feature and closed the gaps found:

- **Test coverage for the query API** (`logs_test.go`): owner scoping, the
  level/category/search filters, pagination, the oversized-limit clamp to 100,
  the session requirement, method rejection, and the audit/errors views. The
  query endpoints were previously only exercised indirectly through an owner
  isolation test.
- **Trace a single request**: the viewer now filters by `request_id` and shows
  both `request_id` and `correlation_id` in the detail row. The backend already
  supported them; the UI exposed neither.
- **Removed the duplicate viewer**: the "System Logs" modal is gone from the
  shell, and the user-menu "Logs" item deep-links to Administration → Settings.
  This also deleted the modal's debounced search timer — the onMount cleanup
  referenced it, so it was the only reason that line existed. Modal-only CSS
  (`.logs-*`, `.log-ts`/`-level`/`-category`/`-event`/`-device`/`-message`/
  `-meta`, `.level-*`) was dropped from `app.css`. `.log-entry` was **kept**:
  `SystemMetrics` relies on it for its bottom border and line height.
- **Authorization documented, not changed**: the operator confirmed the
  deployment is single-user, so no role model was added. `SECURITY.md` now states
  that there are no roles, every authenticated account is an operator, and the
  log settings are the one global setting — with the consequence spelled out if
  public registration is ever enabled.

Verification of the follow-up: server tests green (including the new
`TestLogs*`); web 19 files / **145 tests**, `svelte-check` 0 errors, `vite build`
clean.
