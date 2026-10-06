# Context: dashboard redesign (Rigsight)

Status: frontend complete and verified on `feat/rigsight-dashboard`. Backend
started — services and ports are implemented; see `system-inventory.md`.

## What this work is

A visual redesign of the web dashboard into a navigation-first panel: a fixed
left sidebar owns every route, selecting a device scopes the panel, and nothing
opens as a popup. Reference language is a near-black surface, one mint accent
for healthy/selected state, amber/red/grey for status, and one separate colour
per chart series.

## State

Branch `feat/rigsight-dashboard`, based on `main`.

Commits (oldest first):

| Commit | Subject |
|---|---|
| `81cf7cc` | fix(web): zero-timestamp formatting and clipped telemetry charts |
| `c64bde7` | feat(web): app shell with sidebar navigation and shared UI primitives |
| `55d9bf6` | feat(web): fleet Home screen wired into the shell |
| `32f890a` | feat(web): device overview screen with gauges and history |
| `5b87b91` | refactor(web): use the shared insights helper in App.svelte |
| `cab4f7b` | docs(architecture): navigation-first dashboard shell |
| `a3a3d97` | docs(agents): new web directories in the repo structure |
| `759c313` | feat(web): remaining fleet and device screens |
| `492b99e` | docs: refresh the component list for the new screens |
| `b07185c` | feat(web): real history for the overview period selector |
| `89c70ee` | fix(web): resolve type errors and accessibility warnings |

## Verification (last run on `89c70ee`)

- `cd web && npm run check` → **0 errors, 14 warnings** (main has 15).
- `cd web && npx vite build` → 180 modules, clean.
- `cd web && npx vitest run` → 54 tests passing.
- Server Go tests via Docker `golang:1.25` → all packages `ok`.
- Agent Go tests via Docker → all packages `ok`.
- **No `.go` file changed on this branch**, so the Go suites cannot regress here.

## The gate that actually matters

`vite build` alone is **not** sufficient in this repo. It passed on code that
had 16 type errors and on a Svelte attribute bug. Use both:

```
cd web && npx vite build && npm run check && npx vitest run
```

`npm run check` tolerates warnings (it exits 0) but fails on errors.
`svelte-check` also did not catch an unclosed Svelte block during this work;
`vite build` did. Neither tool alone is enough — run both.

## Defects found and fixed during the redesign

- **`"739893d ago"` on Last sync.** Root cause: Go marshals an unset
  `time.Time` as `"0001-01-01T00:00:00Z"`, which is a *valid* RFC3339 string.
  It passed every truthiness guard, `Date.parse` succeeded, and the arithmetic
  produced ~739893 days. `lib/format.ts` now rejects that shape explicitly.
- **`selectedId` passed without braces** in `Sidebar.svelte`. In Svelte an
  attribute without braces is a boolean literal `true`, not the variable, so
  every device row received `selectedId={true}`.
- **Literal `{id}` inside quoted attribute values** (`Packages`, `Services`).
  Invalid Svelte; parsed as an expression. Escaped to HTML entities.
- **Three conflicting copies of `Device` / `HardwareStats`** in `App.svelte`,
  `DeviceModal.svelte` and `SystemMetrics.svelte`. Now import `lib/types.ts`.

## Known gaps (not defects, deliberate)

- `cpu`, `memory`, `network`, `sensors` all render the same `DeviceOverview`.
  The plan wants per-section views.
- `containers`, `logs`, `security` resolve to `DeviceModal` tabs (page variant,
  not popups).
- Four screens show "not collected yet" states for parts with no API:
  `Services` (systemd units, open ports), `RemoteActions` (reboot /
  update-packages / restart-agent), the pending-updates half of `Packages`, and
  the SMART half of `Storage`. Each names its missing endpoint. `lib/flags.ts`
  (`VITE_MOCK_*`) swaps in labelled sample data for layout review.

## Next steps

Ordered. Each is independent enough to be its own commit or PR.

1. **Open the dashboard in a browser.** Not yet done, and the largest unknown:
   ten screens compile and pass every check but none has been seen running.
   `make dev`, then click through every sidebar entry. Layout and runtime faults
   that type-check cleanly only surface here.
2. ~~Finish the memory/docs handoff.~~ Done: this file and the redesign session
   are both referenced from `index.yaml`.
3. **Per-section device views.** `cpu`, `memory`, `network` and `sensors` all
   fall through to `DeviceOverview`. Give each its own screen, or remove the
   redundant sidebar entries.
4. **Retire the `DeviceModal` tab dependency.** `containers`, `logs` and
   `security` still render `DeviceModal`'s tabs (inline, not as a popup). Extract
   them into `screens/` components so the modal can eventually be deleted.
5. **Tests for the new screens.** Only `format`, `insights`, `api`,
   `telemetry-store` and `telemetry-cache` have unit tests. The ten screens have
   none.
6. **Backend — services and ports done, rest pending.** Implemented in
   `34e9f15`; details in `system-inventory.md`. Still missing, all tracked in
   `ROADMAP.md` under "Pendências de backend conhecidas":
   - endpoints: `GET /smart`, `GET /updates`, `GET /alerts` (+ ack),
     `PATCH /devices/{id}` (rename/tags), `POST /devices/{id}/actions`
   - agent fields: CPU clock (MHz) and core voltage (V)
   - new collector: `agent/collectors/gpu_nvidia.go` (nvidia-smi: load, power,
     hotspot, memory junction, fan RPM, core clock, VRAM)

   Consequence for this branch: `Services.svelte` still renders its "not
   collected yet" state and names `/services` and `/ports` as missing. Both now
   exist, so the screen has to consume them and `VITE_MOCK_SERVICES` /
   `VITE_MOCK_PORTS` lose their reason to exist.
7. **Optional UI honesty check.** `telemetry-store.ts` still caps at
   `MAX_POINTS = 120`. `DeviceOverview` now fetches real history for the longer
   ranges, so the cap only affects the live 5-minute view — confirm that reads
   correctly in the browser.
