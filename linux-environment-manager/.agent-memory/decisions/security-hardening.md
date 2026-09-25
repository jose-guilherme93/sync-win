---
id: decisions/security-hardening
type: decision
title: "Fail-closed feature flags for unsafe capabilities"
description: >-
  Reconnect-by-fingerprint, legacy install and remote mutation capabilities are
  disabled by default until their security controls are implemented.
tags: [security, feature-flags, p0]
source: other
created: 2026-09-25
updated: 2026-09-25
status: active
---

## Decision

Use opt-in environment feature flags with secure defaults:

- `LEM_ENABLE_FINGERPRINT_RECONNECT=false`
- `LEM_ENABLE_LEGACY_INSTALL=false`
- `LEM_ENABLE_REMOTE_MUTATIONS=false`
- `LEM_ENABLE_DOCKER_MUTATIONS=false`

CORS is same-origin by default and only permits an explicitly configured exact origin through `LEM_CORS_ALLOWED_ORIGIN`.

## Why

The current codebase has security-sensitive paths that are not yet ready for production exposure. Fail-closed flags reduce attack surface while preserving an explicit path for controlled development and later migration.

## How to apply

Keep flags disabled in production defaults. Re-enable a capability only after its phase-specific tests and security review pass. Do not use feature flags as a substitute for fixing authorization, recovery or command-policy logic.
