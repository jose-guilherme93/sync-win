---
id: context/p0-remediation
type: context
title: "P0 security remediation initiative"
description: >-
  Active phased remediation plan: solve easy safeguards together, then execute
  one complex security phase at a time and report the next phase afterward.
tags: [p0, security, remediation, workflow]
source: other
created: 2026-09-25
updated: 2026-09-25
status: active
expires: 2026-10-25
---

## Workflow

- Execute the easy P0 safeguards as one batch.
- Execute each complex phase separately; do not start the next complex phase before reviewing the current one.
- After completing a phase, report changed areas, test results, remaining risks and the next recommended phase.

## Phase order

1. P0.0 — easy fail-closed safeguards and low-risk validation.
2. P0.1 — strict session/owner authorization boundary.
3. P0.2 — password, session and device-recovery hardening.
4. P0.3 — rate limiting, SSRF and outbound-network controls.
5. P0.4 — agent policy and Docker operation safety.
6. P0.5 — security regression and release gate.
7. P1 — connect preference, app and save sync functionality.
8. P2 — durable storage, migrations, backup and restore.
9. P3 — frontend, end-to-end tests, CI security and documentation alignment.

## Current handoff

P0.0 is complete. The easy safeguard batch added fail-closed feature flags, safer token generation, password/config validation, enrollment TTL alignment, CORS/header safeguards, log path redaction, redacted-log marking and a cleanup-log table fix. Server and agent race tests, web type-check/build, Compose validation and installer syntax validation passed.

The next complex phase is P0.1: strict session authentication and owner isolation. Do not treat P0 as complete until the authorization boundary, credential recovery, SSRF and Docker policy phases are finished.

## User collaboration preference

Keep the user informed in Portuguese, preserve existing user changes, avoid broad rewrites, and state clearly which phase is next.
