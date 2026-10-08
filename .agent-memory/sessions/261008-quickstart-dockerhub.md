---
id: sessions/261008-quickstart-dockerhub
type: session
title: "Making the project installable by anyone: quick start, license, Docker Hub"
description: >-
  The README started with `make prod`, which needs Go and Node to build. It now
  ships a paste-and-run compose file that pulls the published image, plus an MIT
  licence, badges and the cancellation of a 30-minute CI hang.
tags: [readme, compose, dockerhub, license, ci, documentation, session]
source: agent
created: 2026-10-08
updated: 2026-10-08
status: active
---

## What happened

Three worktrees, in order: `sync-win-agent-update` (agent self-update),
`sync-win-ci-timeout` (CI hang), `sync-win-dockerhub` (Docker Hub) and
`sync-win-readme` / `sync-win-quickstart` (documentation). Each was created and
removed per the worktree rule added to `AGENTS.md` during the first of them.

## The CI hang

A run sat at 19 minutes instead of the usual 4m30s. `scripts/e2e.sh` called
`agent-browser` with no `timeout` anywhere, so a wedged CDP session stalled the
suite until the job's `timeout-minutes: 30` gave up. Every `agent-browser` call
is now wrapped in `timeout`, the job is bounded at 10 minutes and each expensive
step has its own limit. A green run is unchanged.

Worth remembering: the *other* jobs were never slow. `Build Web` 26s, `Test Go`
1m4s, `Lint` ~40s, `Build Docker Image` 32s, all in parallel. The e2e suite
(2m51s) is the critical path, and it is what looked slow because it never
finished.

## Docker Hub

The server image now publishes to Docker Hub and is mirrored to GHCR. The mirror
is deliberate: it needs no credentials (the automatic `GITHUB_TOKEN` suffices),
so a rotated or missing Docker Hub secret cannot leave a tag pointing at an image
that was never published. Publishing still happens before tagging.

The credentials are checked **before** the build, so a missing secret costs
seconds rather than a full image build.

## The bug the user's question exposed

The user asked why the agent is not simply carried inside the server image. It
is — and asking that surfaced a real defect: `Dockerfile.server.dev` never
generated `/app/agent-version.txt`, so the server fell back to
`SYNCWIN_AGENT_VERSION`, whose default in `compose.dev.yaml` was the hardcoded
`0.6.1`, while the contract was at `0.6.2`.

The dev server advertised 0.6.1 and served a 0.6.2 binary. The updater compares
the two and refuses a mismatch, so **no agent could update against a dev
server**. Fixed by deriving the version from `contract.json` in the dev build,
as production already did, and removing the stale default.

## Documentation

`compose.yaml` became the paste-and-run file: it pulls the published image,
persists to a named volume, and requires only `SYNCWIN_SECRET_KEY`, reported by
compose with the exact command to generate it. The README was rewritten with
badges, a configuration table, updating, backup/restore, troubleshooting and
security notes.

Two factual errors were corrected along the way:

- both the README and QUICKSTART told readers the agent is a **user** service
  (`systemctl --user`). It installs as a system service under
  `/etc/systemd/system`, so every command in those sections would have failed.
- the README claimed images were published for arm64; the workflow builds
  amd64 only.

`LICENSE` (MIT) was added. Without it the repository is legally
all-rights-reserved, which contradicts being open source for anyone to use.

## Verification

The quick start was executed exactly as the README instructs, not assumed:
downloaded `compose.yaml` from the raw URL in the README, brought it up isolated
on port 8090 with its own volume, and confirmed `/health` returns ok, the
dashboard index and its 477 KB JS bundle load, registration is enabled, and both
register and login return 200.

That is the point of the verification section in `AGENTS.md`: the earlier device
Logs fix had been signed off against rows seeded straight into the database,
bypassing the agent, so the screen looked right while the real path had never
run.

## Not done

- The journal group was confirmed by reading the extended ACL directly
  (`GROUP_OBJ` and `GROUP id=4` have `r`; the agent is in neither), so the
  diagnosis is observed rather than inferred. Applying it to the real device
  still needs `sudo`.
- `/etc/systemd/system/sync-win-agent.service` is missing on that device
  (`Loaded: not-found`, `Active: active`) and nothing established what removed it.
- The path-unit self-update mechanism has never run on real systemd; only unit
  tests cover it.
