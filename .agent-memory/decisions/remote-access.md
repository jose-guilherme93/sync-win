---
id: decisions/remote-access
type: decision
title: "Interactive remote access (SSH tunnel)"
description: >-
  A device-initiated WebSocket tunnel that lets the dashboard open an SSH
  terminal on a device with no listening port, opt-in locally and fail-closed.
tags: [remote-access, ssh, tunnel, security, policy, decision]
source: agent
created: 2026-10-09
updated: 2026-10-09
status: active
---

## Decision

SyncWin gains an opt-in interactive **SSH terminal** to a device. The device
still opens no port: the agent keeps an outbound connection to the server, and
the dashboard relays a terminal through it. Screen sharing (VNC/noVNC) is
explicitly a later phase.

This is a deliberate **product-direction change**. It creates the first
exception to the long-standing rules *"never execute arbitrary shell commands
remotely"* and *"no remote shell except Docker management"*. Those statements
must be rewritten in `AGENTS.md`, `ARCHITECTURE.md`, `SECURITY.md` and
`ROADMAP.md`; the exception is scoped to sshd logins, mediated by the rules
below.

## Model

- **Transport**: agent → server, outbound WebSocket. One persistent **control**
  link per device (presence + "open a session" requests). Each session is its own
  **data** pipe, not multiplexed: with ≤3 concurrent sessions per device that is
  simpler than a stream multiplexer and keeps control traffic free of payload.
- **Agent is the SSH client** (Model A). It dials `127.0.0.1:22`. Because the
  SSH handshake is loopback-local there is no network MITM, so host-key pinning
  is unnecessary. Consequence to record in `SECURITY.md`: the relay sees terminal
  I/O in clear text. The operator accepted this.
- **Ephemeral key**: per session the agent generates an ed25519 keypair and a
  root helper (path-unit, like the updater) appends the public key to the target
  user's `authorized_keys` with `restrict,pty,from="127.0.0.1"` and
  `expiry-time`, then removes it at session end. No password is ever typed.
- **Target user**: the `SUDO_USER` captured at install, overridable with
  `sync-win-agent set ssh-user <name>`; the dashboard offers the device's local
  users at connect time. **Root is out of scope** for now.
- **Consent is local and fail-closed**: `policy.json` `allow_remote_access`
  (default `false`). The dashboard never flips it remotely; it shows the state
  and the command to run on the device, Tailscale-`set` style:
  `sudo sync-win-agent set ssh on`.
- **Policy location**: `/etc/sync-win/policy.json` (the directory the installer
  creates and the hardened unit may write). The old `~/.config/sync-win/...`
  path is read as a legacy fallback only. The `set` command chowns the file to
  the `sync-win` service user, because `loadLocalPolicy` requires mode `0600` and
  the service must be able to read it.
- **Server-side flag**: `SYNCWIN_ENABLE_REMOTE_ACCESS=false` by default, so the
  endpoints fail closed even on a device that opted in.

## Limits

- Idle timeout 15 min on keyboard inactivity; max 3 concurrent sessions per
  device. No detach/resume (tmux-like) in this phase.
- The server **never** names a host or port. The agent always dials its own
  loopback, so a compromised server cannot pivot into the device LAN.
- Every session is audited (owner, device, target user, start, end, reason).
  Terminal content is not recorded.
