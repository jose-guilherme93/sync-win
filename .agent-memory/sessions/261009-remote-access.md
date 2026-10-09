---
id: sessions/261009-remote-access
type: session
title: "Interactive remote access — foundation (slice 1) and tunnel hub (slice 2 core)"
description: >-
  Started the opt-in SSH tunnel. Landed the local capability, the `set` CLI and
  the policy-path move to /etc/sync-win. Landed the server-side session broker
  (control link registry + per-session pairing) with tests. HTTP endpoints,
  agent bridge and dashboard are still to do.
tags: [remote-access, ssh, tunnel, policy, contract, session]
source: agent
created: 2026-10-09
updated: 2026-10-09
status: active
---

## What happened

Worktree `sync-win-remote-access`, branch `feat/remote-access`, based on
`develop` @ `a708a80`.

Design was fixed in an interview with the operator; the durable outcome is in
`decisions/remote-access.md`. It is a product-direction change (first exception
to the "no remote shell" rule), so the project docs still need rewriting.

### Slice 1 — contract, local capability, `set` CLI (device side)

- `contract.json` → `1.6.0`: `allow_remote_access: false` in `policy_defaults`;
  new `remote_access` block (`ssh_host: 127.0.0.1`, `ssh_port: 22`,
  `idle_timeout_seconds: 900`, `max_sessions: 3`); `policy_path` moved to
  `/etc/sync-win/policy.json` with `legacy_policy_path` for the old
  `~/.config/...`.
- Agent `localPolicy` gained `allow_remote_access` and `ssh_user`; helper
  `remoteAccessAllowed`.
- New `sync-win-agent set <ssh|ssh-user> <on|off|user>`. Writes over the
  fail-closed defaults so it never zeroes other capabilities, atomically at
  `0600`, and chowns to `sync-win` when run as root. `SYNCWIN_POLICY_PATH`
  overrides the path for tests.
- Tests: `TestSetCommandTogglesRemoteAccess` (toggle, persistence, no-clobber,
  rejects). `go build`/`test`/`vet`/`gofmt` clean.

### Slice 2 — server tunnel hub and HTTP wiring

- `server/internal/tunnel`: `Hub` tracks one control `AgentLink` per device and
  pairs a `RequestSession` ticket with the `SessionConn` the agent delivers.
  `SessionConn` is a plain byte pipe (`io.ReadWriteCloser`); the hub is
  transport-agnostic. `ws.go` adapts `github.com/coder/websocket`. Tests cover
  pairing, wrong-device and reuse rejection, unknown ticket, agent replacement.
- `internal/app/remote_access.go`: `POST .../remote-access/session` issues the
  ticket; `GET .../tunnel` is the agent control WS; `GET .../tunnel/session` is
  the agent data WS; `GET .../terminal` is the browser WS and relays bytes.
- `featureFlags.EnableRemoteAccess` (`SYNCWIN_ENABLE_REMOTE_ACCESS`, default
  false).
- **Middleware fix**: WebSocket upgrades passed through `logging` and `gzip`, so
  both wrappers now expose `http.Hijacker` (and `gzipResponseWriter` an
  `Unwrap`); gzip also skips upgrade requests. Without this the handshake found
  no Hijacker and failed.

### Slice 3 — agent bridge and root helper

- `agent/internal/remote`: the privileged half. `InjectUser`/`RevokeUser`
  compose a single restricted `authorized_keys` line
  (`restrict,pty,from="127.0.0.1",expiry-time=...` + a `syncwin:<id>` marker),
  rebuild the key from validated fields only (no comment, no option injection),
  and write atomically. `Helper.Serve` listens on a Unix socket, checks the peer
  uid via `SO_PEERCRED`, validates every request and touches only that line.
  `Call`/`Inject`/`Revoke` are the client.
- `agent/cmd/agent/remote_access.go`: outbound control tunnel with backoff; on
  "open" it checks local policy, generates an ed25519 key with `ssh-keygen`,
  injects it through the helper, dials the session socket, runs `ssh -tt` and
  pumps bytes, with a 15-min keyboard-idle timeout and ≤3 sessions.
- `agent/cmd/agent/helper.go`: `sync-win-agent helper` subcommand (the root
  service). `set ssh on|off` / `set ssh-user` remain the opt-in path.
- `server/sync-win-agent-helper.service`: runs as `root` with `Group=sync-win`,
  `RuntimeDirectory=sync-win`, `ReadWritePaths=/home /root`, otherwise hardened
  like the agent unit. Registered in `agentUnitFiles` and copied in the
  Dockerfile.
- Installer: downloads and enables the helper, checks for `openssh-client`, and
  records `SUDO_USER` as the default target with `set ssh-user` (never enables
  access itself). All non-fatal, per the repo's install rule.

### Slice 4 — dashboard

- `RemoteActions.svelte` renamed to `RemoteAccess.svelte` (and its test);
  sidebar label "Remote actions" → "Remote access". The action commands
  (restart-agent / update-packages / reboot) stay in the same screen.
- Added an interactive SSH terminal card: a user field, an "Open terminal"
  button that POSTs `.../remote-access/session`, then redeems the ticket on a
  same-origin WebSocket rendered by `@xterm/xterm` + `@xterm/addon-fit`.

### Slice 5 — target user dropdown

- Agent contract: `system_inventory.users` (`max_users`, `min_uid`).
- Agent: `collectors.CollectLoginUsers` filters `/etc/passwd` to real login
  accounts (uid ≥ 1000, real shell, no root/nobody). `syncSystemInventory`
  reports `{enabled, default_user, logins}` from the local policy so the
  dashboard knows the device's state.
- Server: `login_users_json` column, `UpdateLoginUsers`/`GetLoginUsers` with
  normalization, and `GET /api/devices/{id}/remote-access` returning
  `{server_enabled, device_enabled, default_user, users, connected}`.
- Dashboard: fetches that endpoint and renders a `<select>` of the reported
  accounts — replacing the free-text field.

### Slice 6 — install screen, audit, docs

- Install modal gained a "Capabilities" section (copyable
  `sudo sync-win-agent set ssh on` / `update`) instead of folding flags into the
  URL.
- Audit: `remote_sessions` table, `StartRemoteSession`/`EndRemoteSession`/
  `ListRemoteSessions`, a row written only once a session connects, and
  `GET /api/devices/{id}/remote-access/sessions`. The dashboard lists recent
  sessions.
- Docs rewritten: `AGENTS.md`, `ARCHITECTURE.md`, `SECURITY.md`, `ROADMAP.md`
  (FASE 24), `COLLECTION-CONTRACT.md`, `API.md`, `README.md` — all record the
  scoped product exception and the dual gating.

### Slice 7 — real end-to-end run with Chromium

Driven with `agent-browser` (Chrome 155) against the dev stack, with a
self-contained "device" container: Debian + sshd + the agent binary running as
`tester` + the root helper, dialing the host's server.

What worked, unchecked: sign in → select device → Remote access → the user
dropdown populated with `tester` (from the agent's `/etc/passwd` scan) →
**Open terminal** → connected → `echo E2E_REMOTE_MARKER_42` executed over the
real sshd and its output rendered in xterm → **Disconnect** → the audit list
showed `tester … closed`.

Three real bugs this found and fixed:

1. **Stale `web_node_modules` volume** (ops): the dev container mounted a named
   volume created before `@xterm/xterm` was added, so Vite could not resolve the
   import and showed its error overlay = black screen. Fix: `npm install` in the
   container / recreate the volume.
2. **Vite proxy had no `ws: true`** (`web/vite.config.ts`): the terminal
   WebSocket under `/api` was never upgraded in dev. Fixed.
3. **Agent dropped the `users` section** when `services`/`ports` both failed:
   the early return only checked those two, so on a host without systemd the
   user list never reached the server. Fixed the condition.
4. **Server rejected the terminal Origin** through the dev proxy: `changeOrigin`
   rewrites `Host`, so the browser `Origin` no longer matched and
   `websocket.Accept` returned 403. Fixed with `OriginPatterns` from
   `SYNCWIN_CORS_ALLOWED_ORIGIN`; the e2e test now dials with a mismatching
   Origin to guard it.

`scripts/e2e.sh` was updated for the `Remote access` rename and asserts the
terminal controls render (the live session itself needs a real agent, so it
stays a manual/e2e-container check).

## Not done

- Real-host test of the helper unit and the installer path (needs root; the
  container run above exercises the same code with root inside the container).
- Phase 2: screen sharing (VNC/noVNC).

## Verification

- Agent: `go build`/`test`/`vet`/`gofmt` clean.
- Server: full `go test ./...` green (app 49s, store 55s); `go vet`/`gofmt`
  clean. `TestRemoteAccessEndToEnd` boots the real middleware chain, upgrades
  WebSockets, pairs the agent data pipe, and asserts an echo round-trip.
- Both modules run in `golang:1.27-alpine` (no local Go toolchain).
