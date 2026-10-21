---
id: sessions/261021-dev-signin
type: session
title: "Fixed dev sign-in and a dev database that kept its devices"
description: >-
  A worktree stack pointed at an empty data-dev, so the existing test devices
  vanished, and the browser suite registered a new e2e account per run. The dev
  stack now signs in with a fixed account and token, and the suite reuses and
  removes one device.
tags: [development, auth, dev-token, e2e, data-dev, docker]
source: agent
created: 2026-10-21
updated: 2026-10-21
status: active
---

## What went wrong

1. **The worktree stack had its own database.** `compose.dev.yaml` binds
   `./data-dev`, which resolves next to the compose file, so the worktree
   created an empty one. The three test devices (`alpine-test-1`,
   `alpine-test-2`, `ubuntu-full-1`) live in the main checkout's
   `data-dev/sync-win.db` and were never in the stack the user opened.
2. **The seeded login was new.** The worktree database had no accounts, so a
   `tabs-check@example.com` account was created for browser checks. That account
   owned nothing, which is why the dashboard looked empty.
3. **The browser suite wrote to the shared dev database.** Every run registered
   `e2e-<pid>@example.com` and enrolled `e2e-laptop-<pid>`, leaving dozens of
   accounts and device rows behind.

## What changed

- **Fixed dev sign-in.** `compose.dev.yaml` now bootstraps
  `demo@sync-win.local` / `sync-win-demo-password` via
  `SYNCWIN_ADMIN_EMAIL`/`SYNCWIN_ADMIN_PASSWORD` and sets
  `SYNCWIN_DEV_TOKEN` (default `sync-win-dev-token`). The web service gets the
  same value as `VITE_DEV_TOKEN`.
- **`POST /api/auth/dev`** trades that token for a normal session cookie. It
  returns 404 unless `SYNCWIN_ENV=development` and the token is set, compares
  digests in constant time, is rate-limited, and never creates an account — a
  missing account is a 503 that names the fix. The dashboard calls it before
  showing the sign-in screen, so dev opens straight onto the fleet.
- **The e2e suite** signs in with the demo account through the normal login
  endpoint, enrolls one stable `e2e-laptop` with `machine_id: e2e-suite`, and
  deletes that device in its exit trap. The trap needs `X-SYNCWIN-CSRF` from
  the cookie jar: `/api/agent/*` is CSRF-exempt, `/api/devices/*` is not.
- **`make dev-account`** is idempotent (login first, register if missing) and
  is only needed after a wiped database.

## Real SSH on the full dev device

`make dev-agents-full` now exercises Remote access for real:

- `docker/Dockerfile.test-device` installs `openssh-server`, creates
  `devuser`, generates host keys and enables `ssh.service`.
- `scripts/dev-test-agents.sh full` installs and starts
  `sync-win-agent-helper.service` (agent runs as root there, so
  `--user root --group root`), grants consent the way an owner would
  (`sync-win-agent set ssh on`, then `set ssh-user devuser`), and starts sshd.
- The image is rebuilt on every run. The script used to build only when the
  image was missing, and `docker rmi` had failed because the container still
  used it, so the new Dockerfile was silently ignored and the container had no
  sshd at all.

## To test a worktree against the real dev data

The compose bind is relative to the compose file, so point `data-dev` at the
main checkout instead of copying the database:

```bash
make dev-down
mv data-dev /tmp/data-dev-backup
ln -s /home/joseti/projects/sync-win/data-dev data-dev
make dev-d
```

A symlink is not covered by `.gitignore`'s `/data-dev/`, so it shows as an
untracked path; never stage it, and prefer a checkout-local stack for anything
that must not touch the real dev data.

## Verification

- Server: 6 new tests for the token (development-only gating, disabled/wrong
  token, session cookie usable on `/api/auth/me`, no account creation).
  `go test ./...` green.
- Web: 4 tests for `signInWithDevToken`; 26 files / 243 tests, svelte-check 0
  errors, build clean.
- Browser: `http://localhost:5273` opened with no sign-in form, Fleet rendered,
  and the three devices reported online again after `scripts/dev-test-agents.sh
  up` and `full`.
- E2E: 73/73 checks, with the login-screen assertion inverted to "dev stack
  signs in without the login screen"; the fleet was back to exactly the three
  test devices after the run, proving the cleanup ran.
- Live SSH: the browser opened a `devuser` session on `ubuntu-full-1`, `whoami`
  answered `devuser`, a second tab ran its own command, and a third brought the
  card to `3 of 3 open · 3 connected`. Tab 1's scrollback stayed free of tab 2's
  output.
