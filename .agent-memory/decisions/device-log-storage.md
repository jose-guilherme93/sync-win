# Decision: device logs get their own table

## The problem

The dashboard had a device Logs screen. It rendered a list and it always showed
nothing. The screen was not the bug — it read a field that was almost always
empty, and reported that honestly ("the agent has not reported any device logs
yet").

Three defects stacked up.

### 1. The server erased the batch five cycles out of six

The agent samples the journal on one telemetry cycle in six
(`logCollectCycles = 6`, about 60s at the default 10s interval), because
`journalctl` is the heaviest collector in the cycle. So `stats.Logs` is nil on
the other five cycles.

`UpdateHardwareStats` marshals the whole `HardwareStats` struct into the
`hardware_json` column and `UPDATE`s it wholesale. `Logs` is tagged
`json:"logs,omitempty"`, so nil logs meant the key was absent from the JSON —
and the next write replaced the row with a payload that no longer contained it.

The screen read `data.hardware.logs` from `/api/devices/{id}/detail`, so it
saw a batch for roughly one cycle in six and empty for the rest. Confirmed on
the live dev database: device `hp240-debian` had a 5.7 KB `hardware_json`
containing zero log entries.

Not a collection problem. `journalctl` runs fine as the unprivileged `sync-win`
service user; that was verified directly.

### 2. Nothing could query them

Even a surviving batch was not a log history. Lines lived only inside that blob,
so there was no retention, no severity/source/time filtering, and no paging.
The viewer could show at most the newest ~150 lines and nothing older.

### 3. The agent failed silently

`CollectDeviceLogs` returned `nil` when `journalctl` failed. The agent runs
unprivileged, so an unreadable journal is a real deployment outcome (no
journald, no read permission, non-systemd host). A silent nil is
indistinguishable from a device with nothing to report, which is what made the
original symptom so hard to diagnose.

## The decision

Journal lines get their own table and their own endpoint.

- `device_logs` with a unique index on `(device_id, ts, source, message)`.
  Ingestion is `INSERT OR IGNORE`, so the overlapping windows an agent re-ships
  every minute cost one cheap insert each and store once.
- `GET /api/devices/{id}/logs` with `level`, `source`, `search`, `since`,
  `limit`, `offset`. Filtering is server-side because that is where the rows
  are; the client only dims rows the active filters exclude.
- A startup-then-daily retention pass, 7 days. Ingestion is append-only, so
  without it the table grows without bound.
- `CollectDeviceLogs` returns `(logs, error)` and the agent logs the failure,
  so an unreadable journal shows up in the journal the viewer is reading.

The `logs[]` field still travels inside the telemetry payload. A dedicated
ingest endpoint for a once-a-minute batch would add a second failure path for
no gain, and the agent already has a working transport.

## Why the viewer is one component

`DeviceLogs.svelte` is rendered by both the sidebar Logs section and the device
modal tab. Those were separate before, and they drifted. `web/src/lib/types.ts`
exists precisely because duplicated shapes with slightly different fields had
already produced two components formatting the same timestamp two different
ways. One component cannot drift from itself.

## Rejected

**Preserve the previous `logs` when the agent sends none.** A few lines in
`UpdateHardwareStats`, and it would have made the screen non-empty. It still
caps the view at one 150-line sample with no history and no server-side
filtering, which does not meet "robust and functional".

**SSE or cursor pagination for a live tail.** The screen uses `limit`/`offset`,
which suits loading and filtering. Tail-follow on `offset` degrades as history
grows, so a real follow mode wants a cursor. Not built: the agent samples once
a minute, so anything faster than the existing 60s poll would be speculative.

## Consequences

- `hardware_json` shrinks: it no longer carries a 150-line log batch.
- The store gained a table with a foreign-key-free relationship to `devices`, so
  `DeleteDevice` deletes its lines explicitly. Done inline in that function
  because the locked helper in `device_logs.go` would deadlock on the same
  non-reentrant mutex.
- Server log retention and device log retention are different knobs. Device logs
  are diagnostic; the server log table is closer to an audit trail.

## Verification

`server/internal/store/device_logs_test.go` includes a regression test named for
the original bug: append one line, run five log-less telemetry cycles through
`UpdateHardwareStats`, assert the line survives.

## Related known issue, not fixed here

`scanDevice` scans nullable columns (`last_sync_at`, `last_error_at`,
`services_json`, `ports_json`) into plain `string`, so a `NULL` there makes
`/api/devices` fail with a 500. Normal writers use empty strings so it is
unreachable in production, but it bit a seed script during this work. Worth
`COALESCE` in the query or a `sql.NullString` scan.