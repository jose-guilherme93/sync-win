---
id: sessions/261011-merge-and-release
type: session
title: "Landing dev/tailnet-access on develop and main, and the CI it had never run"
description: >-
  Merged the long-lived, never-pushed dev/tailnet-access branch plus the UI pass
  into develop and main, released 0.9.0 and 0.9.1, and fixed the three classes of
  failure that only became visible once CI could finally run against it.
tags: [release, ci, e2e, lint, merge, agent-version]
source: agent
created: 2026-10-11
updated: 2026-10-11
status: active
---

## What was landed

`dev/tailnet-access` had 16 commits that had never been pushed, and
`feat/ui-polish` had 8 more on top of it. Local `develop` was itself 6 behind
`origin/develop`, so it was fast-forwarded first. Everything was merged as
explicit merge commits, matching how develop already integrates branches.

`origin/develop` is an ancestor of both, so the merges were content-trivial and
the result tree is byte-identical to the verified branch. Every other branch
(`feat/rigsight-dashboard`, `feat/dashboard-rework`, `feat/install-prompt`,
`feat/remote-access`, `fix/remote-access-enable`, `feat/dashboard-functionality`)
was already contained in `dev/tailnet-access` and came along with it.

`origin/main`'s tree is identical to the merge base, so the merge into main
landed exactly develop's tree with no conflicts.

## The lesson: an unpushed branch rots its own e2e suite

Three classes of failure were invisible until the push, because **CI had never
run against this branch**:

1. `golangci-lint` (server): `store.go` used `db.Query`/`QueryRow`/`Exec` instead
   of the Context variants, and ignored three `rows.Close` errors. `errcheck`'s
   `(io.Closer).Close` exclusion does **not** cover `*sql.Rows`, so the close
   errors are real. Fixed with a helper that closes rows on every path.
2. The e2e job could not start the dev stack: the agent-signing work made
   `.dev-keys/agent_update_key` a **required compose secret**, and the job ran
   `docker compose up` without generating it. It now runs `make dev-key`, the
   same prerequisite `make dev` carries.
3. Six `scripts/e2e.sh` assertions had been failing since the dedicated-screens
   rework, which rewrote the Services screen and renamed a nav label without
   updating the script.

The third is the one worth remembering. It was confirmed by diffing the
assertions against `ba6dbc5`, the last commit whose e2e run was green: Services
had `class="summary"` then and `class="tiles"` now; the port row was
`proto addr port` then and `port proto addr` now; the nav label was
"Containers" then and "Docker" now; and the Services screen had its own Refresh
control, which is what the failure-state check clicked. None of that was caused
by the work being merged — it had simply been broken, silently, for as long as
the branch stayed local.

**A green local suite is not evidence the suite is current.** `npm run test` and
`svelte-check` cannot see a script that asserts on markup the redesign replaced.

## Agent version

`agent_version` in `contract.json` is hand-maintained and bumped with every
agent change (`0.7.0 → 0.7.1`, `0.6.1 → 0.6.2`). The socket de-duplication and
the apt version join had not bumped it, so a device already on 0.7.1 would
compare equal, never update, and never receive either fix. Bumped to **0.7.2**;
the binary reports it and the served version comes from the same file.

## Releases

- **0.9.0** — from the first push to main. Its Release job was green; its CI was
  red (the three failures above), which is how they were found.
- **0.9.1** — the CI fixes. Product-identical to 0.9.0; only lint, the CI
  workflow and the e2e script changed. Pushed deliberately so main is not left
  red and inconsistent with develop.

Both were verified green afterwards: develop CI and main CI, all seven jobs
including the browser end-to-end.

## Verification

- `golangci-lint v2.14.0 run --new-from-rev=2110335`: 0 issues in both modules,
  run the way CI does.
- `scripts/e2e.sh` against a local dev stack: **all 59 checks pass**.
- server and agent `go test ./...`, `go vet`, `gofmt`: clean.
- web: 138 tests, `svelte-check` 0 errors, `vite build` clean.

## Environment notes

- The main checkout's `web/node_modules` was stale (missing `@xterm/*`), which
  made the suite look broken until `npm ci`. A stale install is not a code fault.
- `web/dist/assets` is **root-owned** from an old container build, so
  `vite build` fails locally with `EACCES` when emptying the output directory.
  It needs `sudo rm -rf web/dist/assets` once. CI is unaffected.
- `agent/collectors/network.go` is gofmt-dirty on develop and was before this
  work. It is in the merge range, which is why it was fixed as a `style:` commit
  rather than left to fail lint.
