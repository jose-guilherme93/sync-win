---
id: context/p0-remediation
type: context
title: "P0 security remediation (complete)"
description: >-
  The phased P0 security remediation is finished and released. Kept as a record
  of what shipped and of the verification gaps that remain operational; there is
  no open P0 phase.
tags: [p0, security, remediation, completed]
source: other
created: 2026-09-25
updated: 2026-10-09
status: completed
---

## Status

P0 is complete. It shipped as `5bd1057 security: complete P0 remediation`
(2026-09-25, 59 files, +2620/-699) and is part of the released `v0.6.10`.
`ROADMAP.md` and `SECURITY.md` document the delivered controls.

This file previously described P0.1–P0.5 as pending, while they had already
shipped. That stale handoff misled an agent on 2026-10-09 into planning work
that was already done. Do not reintroduce a "next phase" section here; if new
security work is needed, open a new context file for it.

## What shipped

- P0.0 — fail-closed feature flags, safer token generation, password/config
  validation, CORS/header safeguards, log redaction, enrollment TTL alignment.
- P0.1 — strict session authentication and owner isolation (`owner_id` is
  enforced in the store; client-supplied owner IDs are never authorization).
- P0.2 — Argon2id password hashing, hashed session/device tokens, HttpOnly
  cookie, CSRF, session revocation.
- P0.3 — rate limiting and SSRF/webhook protection (`rate_limiter.go`, pinned
  webhook IP, disabled redirects).
- P0.4 — local fail-closed policy, Docker limits, timeouts and device-scoped
  command results.
- P0.5 — security regression tests and aligned documentation.

## Verification gaps still open (operational, not code)

These need a real host and `sudo`; they are not code patches.

- The journal group ACL was diagnosed but never applied on a real device.
- The path-unit self-update mechanism has never run on real systemd.
- `/etc/systemd/system/sync-win-agent.service` was missing on the inspected
  device and nothing established what removed it.

## Related

- `decisions/security-hardening.md` — the fail-closed flag decision.
- `sessions/260925-p0-start.md` — the P0.0 kickoff.
- `sessions/261009-p0-audit.md` — the 2026-10-09 re-audit that verified
  P0.1-P0.5 against a running server and hardened the webhook SSRF filter.
