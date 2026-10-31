---
id: sessions/261030-access-token
type: session
title: "Replacing accounts with a single dashboard access token"
description: >-
  What changed when the account system became one token from the environment,
  and what was verified before the branch was pushed.
tags: [auth, security, brand, session]
source: agent
created: 2026-10-30
updated: 2026-10-30
status: active
---

## What landed

- `SYNCWIN_ACCESS_TOKEN` replaces email/password login, registration and the
  admin bootstrap. See `decisions/dashboard-access-token.md`.
- `POST /api/auth/login` takes `{"token"}`, compares digests in constant time and
  issues a session for the single owner row (`Store.EnsureOwner`).
- Removed: `/api/auth/register`, `/api/auth/config`, `/api/auth/dev`,
  `/api/auth/update-email`, `/api/auth/update-password`, `bootstrapAdmin`, the
  registration flag, and the profile forms in the dashboard.
- The sign-in screen now uses the app's tokens and the topology glow instead of
  the legacy blue card. It asks for the token only.
- The SyncWin mark (two nodes joined by a transfer path) is the favicon, the app
  icon and the sidebar/login lockup.

## Verification

- `go test ./...` and `npm run test` (327) green; `svelte-check` 0 errors.
- Against the running dev stack: wrong token → `401`, right token → `200` with
  `owner_id`; the SPA auto-signs in through `VITE_ACCESS_TOKEN`; the gate was
  rendered and inspected with the token field and the mint glow.

## Not done here

- The navigation redesign (Home = fleet only, device screens full-width with
  horizontal tabs) and the per-device opt-in agent update are still open.
