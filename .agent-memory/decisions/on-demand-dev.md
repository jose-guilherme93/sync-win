---
id: decisions/on-demand-dev
type: decision
title: "Dev stack, test agents and browser verification run only on demand"
description: >-
  The development stack and its agents are not left running when nobody is
  using them, the dashboard is not exposed on the tailnet by default, and
  browser checks are one-shot with low-memory Chrome flags.
tags: [dev, resources, ram, tailnet, browser, verification]
source: agent
created: 2026-10-19
updated: 2026-10-19
status: active
---

## Decision

- Do not keep the dev stack (`sync-win-dev` server and web) or the
  `sync-win-test-agent-*` containers running when nobody is actively working on
  the code. Start them on request, stop them with `make dev-down` and
  `make dev-agents-down` when finished. Device records in `data-dev/` stay.
- Do not serve the dashboard over the tailnet by default. Use
  `make dev-tailnet` only when the user explicitly asks for a tailnet URL, and
  take it down when the review is over.
- Browser verification is one-shot: check free memory first, launch with
  low-memory flags (`--no-sandbox`, `--disable-gpu`, `--disable-dev-shm-usage`,
  `--renderer-process-limit=2`, `--js-flags=--max-old-space-size=128`), run all
  steps for that check, and close every session in the same command even when a
  step fails. Never leave an `agent-browser`/Chrome daemon running between
  checks.
- Run the full e2e suite only when explicitly requested. Routine verification
  uses API/curl, unit tests, `svelte-check`, `go test` and the build; screenshots
  are reserved for the final visual check.
- Do not start a second browser or rebuild the server image while a check is in
  progress. One browser, one build at a time.

## Why

The VPS has about 5.8 GB of RAM and already runs other services. Keeping a
headless Chrome, the dev stack, test agents and repeated image builds alive at
the same time pushed the machine into swap. The dev stack is a development
convenience; production runs the published image, so nothing needs to listen
between sessions.
