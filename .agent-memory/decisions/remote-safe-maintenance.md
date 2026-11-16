---
id: decisions/remote-safe-maintenance
type: decision
title: "Maintenance never runs inside the session it restarts"
description: >-
  An install or update started from a SyncWin remote terminal must move itself
  behind PID 1, and a rollback must always leave a startable agent.
tags: [remote-access, installer, update, rollback, incident, decision]
source: agent
created: 2026-11-13
updated: 2026-11-13
status: active
---

## The incident

An agent update was run from a SyncWin remote terminal on a device that had no
other way in. It stopped printing, no new terminal could be opened, and the
machine could not be reached at all afterwards. The device's only access path
was the thing being replaced.

## Three faults, each sufficient on its own

1. **The session is a child of the process being replaced.** `pumpSSH` starts
   `ssh -tt` from the agent, so the agent owns the pty. Anything that restarts
   the agent closes that pty and takes the operator's terminal with it. The
   installer did this on purpose: `install_systemd_service` ran
   `systemctl stop` before it had written the new unit.
2. **The rollback deleted the unit.** `ROLLBACK_NEEDED` was set to
   `service_installed` before the write, and `rollback()` did `rm -f` on the
   unit file and stopped. The shell that died with the session ran the EXIT
   trap, so the device ended with a binary and no unit: no agent, no tunnel, no
   second chance. `backup_existing` had already copied the old unit; rollback
   never used the copy.
3. **A control-plane blip killed live sessions.** `readTunnel` ended every
   active session whenever the control websocket reconnected, although sessions
   carry their own data socket. A server restart or a dropped link ended a
   running install mid-way.

## The decision

- A run that finds `SYNCWIN_REMOTE_SESSION` in its environment **re-executes
  itself behind PID 1** with `systemd-run --collect`. The agent restart may drop
  the terminal, but it cannot drop the work. The marker is set by the agent on
  the ssh client, so any shell inside the session inherits it.
- `rollback()` restores the backed-up unit and binary and **restarts** them. It
  removes files only when there was nothing there before this install.
- `install_systemd_service` writes the unit before stopping anything; the only
  restart is `start_service`, and a failed restart rolls back on the spot.
- `readTunnel` no longer ends sessions on reconnect. A session ends when its own
  socket closes or the agent stops.
- `sync-win-agent update` asks `activeSessionCount()` and says how many
  terminals the restart will close.

## The rule this leaves

**Never run a restarting operation from the terminal it restarts.** Every
maintenance path that can take the agent down must first ask whether it is
running inside that agent's session, and must survive the stop if it is.

## The durable half: sessions live in the helper

`remote.SessionHost` runs the terminal in `sync-win-agent-helper`: it generates
the ephemeral key, injects it, dials the attach WebSocket and pumps the pty. The
agent keeps only what it must — the local policy and the target account — and
forwards `open` and `resize` over the helper socket. Consequences:

- an agent restart, its rollback, and its rollback-of-the-rollback all leave the
  terminal running; the dashboard never loses its session;
- the attach destination is fixed by root-owned configuration (the helper's
  environment, or the `--server` in the agent's unit), so the unprivileged agent
  cannot point the root helper at another host;
- the one remaining way to drop a session is a helper unit change, which
  restarts the helper. That is rare, and it is not part of an agent update.

The ticket is the session's identity: `authorized_keys` marker, `Resize` key and
the map key in `SessionHost`. It is 64 hex characters, which is why the helper's
`idPattern` fits it unchanged.

## Recovery when the agent is already gone

Nothing in SyncWin can reach a device whose agent is not running. Console, a
second machine on the same network, or the Tailscale address are the only paths.
Once in, the unit is restored by restoring the backup the installer left, or by
re-running the installer with an up-to-date script.
