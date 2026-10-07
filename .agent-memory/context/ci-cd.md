---
id: context/ci-cd
type: context
title: "CI on develop, and the release path that publishes before tagging"
description: >-
  Why the deployment saw a tag with no image behind it, and the ordering
  change that makes a tag imply a published image.
tags: [ci, cd, release, ghcr, docker, lint, session]
source: agent
created: 2026-10-07
updated: 2026-10-07
status: active
---

## What was broken

The deployment watches the repository tag. The release workflow created the tag
first and treated the image publish as optional, skipping it when Docker Hub
credentials were missing. Since no such secret was configured, the sequence was
always: tag created, no image, deploy attempted against a tag that could not be
pulled.

The container in production runs `sync-win:latest` with no registry digest and
no OCI labels, so it was built locally on the Dokploy host. Nothing in the
repository was publishing an image anywhere.

## The fix

Publish before tagging. The invariant is now **a tag exists implies a matching
image exists**; if the publish fails, no tag is created and no deploy fires.

Images go to GitHub Container Registry authenticated with the automatic
`GITHUB_TOKEN`, so there is no registry password in the repository. Published
for `linux/amd64` and `linux/arm64` with provenance and SBOM attached.

## The upgrade hazard, and why it was caught

Running the server unprivileged is the obvious security fix and it silently
breaks every existing install. A database created by the old root-running image
is `root:root 0644`, so an unprivileged server cannot open it:

    new store: ping sqlite: attempt to write a readonly database (1544)

This was reproduced against a copy of a real 26 MB database before shipping. The
entrypoint (`scripts/docker-entrypoint.sh`) starts as root only to `chown` the
data directory, then `exec`s through `su-exec` so the server is PID 1 as
`syncwin`. `exec` is load-bearing: a shell left in front swallows SIGTERM, and
`docker stop` then exits 137 instead of 0.

The roadmap claimed "non-root user" was done in FASE 13. The Dockerfile never
implemented it. Check claims against the code.

## Lint was unreachable locally

CI uses golangci-lint (errcheck, noctx, misspell, staticcheck, unused, and
govet's shadow analyzer). `make lint` runs `go vet`, which enables none of
those. So the 13 issues could only appear after pushing.

`make lint-go` now runs the same golangci-lint version and diff base as CI.

`--new-from-rev=origin/main` reported **0 issues** and that was wrong: `main`
already contained the work, so the diff was empty. The honest base is the commit
before the change. This is the second time in this project that a passing check
was vacuous rather than green — the first was `only-new-issues` on a
workflow-only commit, and earlier `vite build` plus 63 unit tests on a Packages
screen that rendered nothing.

## Known debt

The full golangci-lint run reports **162 issues** in `server` (errcheck 49,
govet 50, noctx 46, misspell 6, staticcheck 3, unused 8). `only-new-issues`
tolerates them and ratchets. They are paid down as files are touched, not
wholesale.

`govet` runs `enable-all: true`, which enables `shadow`. Six of the thirteen were
the `if err := ...` idiom, a false positive by design.

## Still open

Whether the Dokploy deployment pulls the GHCR image or builds from source
determines whether any of this changes its behaviour. If it builds locally, the
original problem was the GitHub integration, not the missing image.

`AGENT_UPDATE_KEY` must exist as a secret for signed agent updates. Without it
the image still builds and the agent refuses auto-update, which is fail-closed
by design.