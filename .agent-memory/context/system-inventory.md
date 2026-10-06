# Context: system inventory backend (services + ports)

Status: implemented and committed as `34e9f15` on `feat/rigsight-dashboard`.
This was the first of the backend items listed in `dashboard-redesign.md`.

## What it does

The dashboard's `Services` screen had an honest "not collected yet" state naming
`GET /api/devices/{id}/services` and `/ports` as the missing endpoints. Both now
exist, along with the agent upload that feeds them.

## Endpoint and storage decisions

Snapshots live in `services_json` / `ports_json` columns on the `devices` row,
following the existing `apps_json` pattern: whole-value replacement, no new
tables, and deletion follows the device.

Both sections are `*[]T` in the request. This distinction matters and is tested:
an omitted section keeps the last snapshot, while an explicit empty array
replaces it. A transient `systemctl` failure on the agent must not be
indistinguishable from a device that genuinely reports nothing.

`GET` routes require the session owner and reject the device token — they exist
for the dashboard, not the agent. `COALESCE` is required on read because a column
added without a default holds NULL and `Scan(&string)` fails on it.

## Non-obvious platform detail

`systemctl list-units` columns are `UNIT LOAD ACTIVE SUB`. A healthy service
reports `ACTIVE=active` with `SUB=running`, so filtering on the ACTIVE column
against the contract's `running` value discards everything. `unitStatus()` maps
onto the `running`/`failed`/`stopped` buckets the contract and dashboard speak,
and the raw columns are kept alongside it.

Units sort `failed` → `running` → `stopped` before name order, otherwise a broken
unit is buried among hundreds of stopped ones.

## The screen was never wired up

The backend landed first and the screen was still showing its "not collected
yet" state, which named the two endpoints as missing. Two things had to change in
the dashboard for the feature to actually reach a user:

- `App.svelte` rendered `<Services />` with no props. Without a `deviceId` the
  screen had no way to call an endpoint, so it could only ever show sample data
  or an empty state. This was the real blocker, not the missing backend.
- `flags.ts` lost `services` and `ports`. `smart` remains, still without an
  endpoint. `RemoteActions.svelte` had been using `MOCK.services` to decide
  whether to warn that its actions are unimplemented — an unrelated coupling,
  now removed.

Three states are now distinct on the screen: never reported, reported nothing,
and request failed. Conflating the last two would let a 500 read as "this device
has no services".

## Browser verification

`scripts/e2e.sh` (`make test-e2e`), 46 checks. It seeds an account, device and
inventory through the real API, then drives a real browser. Requires `make dev-d`
running and `agent-browser` installed.

Two earlier claims from this work were wrong and are corrected here, because both
came from reading a screenshot instead of measuring:

- The "Without Lynis" card on the fleet view does **not** overflow its grid row.
  All five cards have identical `bottom` and `height`. The amber `.kpi.warn`
  border was misread as a layout fault. The suite now asserts shared bottom
  edges so the question cannot come up again.
- The sidebar buttons were never stuck `disabled`. The fleet row was not clicked;
  the status filter button was.

The suite's own selector choices are deliberate, because getting them wrong
silently tests the wrong thing:

- Element refs go stale on every re-render, and a stale ref silently clicked
  Overview instead of Services. All clicks select the target by its own text
  inside the page.
- `querySelector('input[type=search]')` matches the device search box, which
  appears earlier in the DOM than the screen filters. Filters are addressed by
  `aria-label`.
- `agent-browser eval` prints JSON-encoded output, so `unquote` is applied before
  comparing results.

Verified to actually catch regressions: mutating the screen to render raw
`active_state` instead of the mapped status made two assertions fail.

The suite also reports, without failing, that Overview, CPU, Memory, Network and
All sensors render byte-identical panels. That is the known debt below.

## Verification

```
cd server && docker run --rm -v "$PWD":/src -w /src golang:1.25 sh -c 'gofmt -l . ; go vet ./... && go test ./... -timeout 900s'
cd agent  && docker run --rm -v "$PWD":/src -w /src golang:1.25 sh -c 'gofmt -l . ; go vet ./... && go test ./... -timeout 600s'
cd web    && npx vite build && npm run check && npx vitest run
```

Go is not installed on the host; `docker run golang:1.25` is the only runner.
All three suites pass: 7 new handler tests, 6 new store tests, 9 new collector
tests, and `contract_test.go` now covers the `system_inventory` block, which
previously had no test at all.

The web suite is 63 tests (54 before this change, plus 9 for `Services.svelte`).
That screen is the first of the redesign's ten screens to have any tests. It
still has not been opened in a browser.

## Still open

The remaining backend items are now tracked as an explicit "Pendências de backend
conhecidas" section in `ROADMAP.md` instead of only living in a memory file:
`smart`, `updates`, `alerts` + ack, `PATCH /devices/{id}`,
`POST /devices/{id}/actions`, CPU clock/voltage, and
`agent/collectors/gpu_nvidia.go`.

## Note on this change

Nine defects were found by tests written alongside the code during this work,
including a filter on the wrong systemd column, a parser that matched the process
name on an already-truncated line, a `range` over values that never mutated,
`NULL` failing to scan into a string, a generic type parameter on a method, and
two test fixtures that set up users in the wrong order. None escaped, but the
pattern was consistent: writing too much before compiling. Building after each
file rather than each feature would have been faster.