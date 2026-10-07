---
id: sessions/261007-dashboard-functionality
type: session
title: "Wiring the dashboard's visible controls and hardening the agent download"
description: >-
  Made Lynis tracking, device settings, remote actions, pending updates and the
  Findings screen real; found the install failure was dev hot-reload, not code.
tags: [dashboard, lynis, settings, remote-actions, updates, download, session]
source: agent
created: 2026-10-07
updated: 2026-10-07
status: active
---

## What happened

Worked from the branch state where `develop` was clean at `ce04b64` (the
parallel-agents decision). Because a second agent had already contaminated a
commit via the shared index, this task ran in a dedicated worktree first, then
was consolidated back into the main checkout so the running dev stack could
pick it up.

Implemented, in order:

1. Lynis command tracking (owner-authenticated status read; save report before
   completing the command).
2. Device settings persistence and a remote telemetry interval the agent reads.
3. Remote actions (`restart_agent`, `update_packages`, `reboot_device`) with an
   explicit API, allowlist agent execution and fail-closed policy.
4. Pending updates: read-only collector, server snapshot, showing up in
   `Packages`.
5. A real `Findings` screen, so `Findings` no longer duplicates `Alerts`.
6. Agent download hardening.

## The install bug was not a download bug

The user's `install.sh` failed at step 7 with `curl: (18) transfer closed with
outstanding read data remaining` then `(28) ... 0 bytes received`.

Reproduced the download directly: 9.7 MB, intact, 27 ms, 5/5 attempts. Root
cause found in the dev container logs: `air` restarted the Go server mid
download (shutdown 14:27:39, back at 14:27:50) because Go files under the
bind-mounted `server/` had been edited. During that window docker-proxy accepts
the connection but no process answers, which is exactly the observed symptom.

Fix for the user: retry; production has no `air`. Hardening regardless: the
download now streams with `http.ServeContent` (Content-Length + Range/resume)
and binary responses are no longer gzipped.

## Defects the tests caught

- The pending-update output limiter looked correct but `bytes.Buffer` embedding
  made `io.Writer` calls bypass the `exceeded` flag; replaced with an explicit
  buffer and a byte counter.
- Test fixture typed a partial `HardwareStats`, which `svelte-check` rejects.

## Verification

- server/agent `go test ./...`: ok. web: 81 tests, 0 check errors, clean build.
- Live: `Range` → 206, `Accept-Ranges: bytes`, no `Content-Encoding`.

## Handoff

See `.agent-memory/context/dashboard-functionality.md` for state and the open
list. Nothing is committed. The `feat/dashboard-functionality` branch was left
in place after its worktree was removed; commits should go to whichever branch
the operator chooses, with Conventional Commit messages.
