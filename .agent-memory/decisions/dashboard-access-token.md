---
id: decisions/dashboard-access-token
type: decision
title: "The dashboard is opened with one access token, not an account"
description: >-
  Replacing email/password accounts and registration with a single token from
  the environment, and the owner row that keeps devices attached.
tags: [auth, security, dashboard, config]
source: agent
created: 2026-10-30
updated: 2026-10-30
status: active
---

## Decision

The dashboard has exactly one credential: `SYNCWIN_ACCESS_TOKEN`, read from the
environment, at least 32 characters. `POST /api/auth/login` takes `{"token": …}`
and answers with a session cookie. There is no registration, no email, no
password, no recovery flow and no profile screen.

## Why

The product is single-operator and self-hosted. Accounts existed to separate
owners, but every install has one operator, and the account layer carried a
registration switch, an admin bootstrap, a password lifecycle and three
endpoints whose only job was editing an email or a password. That is surface
area an attacker can use and an operator has to understand, for no isolation
benefit.

## How it stays safe

- The token is never written to the database; sign-in compares SHA-256 digests
  in constant time, so length and prefix do not leak.
- A missing or short token leaves sign-in disabled and `/api/auth/login` answers
  `503` naming the variable. It does **not** fall back to something else, and it
  does **not** stop the server: agents authenticate with their own device tokens
  and must keep syncing while the dashboard is locked.
- Rotating the token and restarting invalidates every browser on its next
  request, which is the whole "log everyone out" story.

## The owner row

`users` still has one row, created on first login by `Store.EnsureOwner`, with a
random discarded password hash. Devices and sessions hang off its id, so
upgrading in place keeps existing installs working and does not orphan enrolled
devices. Nothing authenticates with that row's password; it is an id, not an
account.

## Consequences

- Upgrading from an account-based install locks the dashboard until
  `SYNCWIN_ACCESS_TOKEN` is set. That is deliberate: the old credentials must
  not keep working.
- `SYNCWIN_ENABLE_REGISTRATION`, `SYNCWIN_ADMIN_EMAIL`, `SYNCWIN_ADMIN_PASSWORD`
  and `SYNCWIN_DEV_TOKEN` are gone. The dev stack uses the same token and passes
  it to the SPA as `VITE_ACCESS_TOKEN`.
