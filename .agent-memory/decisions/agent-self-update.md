# Decision: the agent asks to be updated, it does not update itself

## The problem

Fixes could not reach devices. The Logs screen had been "fixed" twice, released
twice, and production stayed exactly the same: every agent still on 0.6.1, no
`logs` key in telemetry, and a `device_logs` table with zero rows.

Three causes stacked:

1. **The unit was wrong.** Journal files are `0640 root:systemd-journal`, and the
   unit gave the agent only `docker`. It could not read the journal, so it
   reported no logs. Forever, on every device.
2. **The updater replaced only the binary.** A change to the unit is invisible to
   a binary-only update, so the group fix above could never arrive unattended.
3. **Nothing was triggering updates at all.** `sync-win-agent-update.timer` was
   `not-found` on the device we inspected. `install_auto_update` treats a missing
   template as `warn … return 0`, so a device could end up with no update path
   and no visible error.

There is a bootstrap deadlock in 2 and 3: the code that makes a unit change
reachable ships inside the update that cannot arrive.

## The constraint that decides the design

```
/usr/local/bin/sync-win-agent   root:root 755       agent cannot replace it
/usr/local/bin                  root:root 755       agent cannot write here
/etc/systemd/system             root                agent cannot write the unit
/var/lib/sync-win               sync-win:sync-win   agent CAN write here
```

The daemon runs as an unprivileged service user. "The agent updates itself" can
therefore never be literal: it cannot replace its own binary or its own unit.

## The decision

The daemon **requests**, a root-owned path unit **acts**.

- The daemon writes `/var/lib/sync-win/update-request` — a directory it owns and
  that is already listed in the unit's `ReadWritePaths`.
- `sync-win-agent-update.path` watches that file and starts the existing
  `sync-win-agent-update.service`, which runs as root.
- The update service reconciles **both** the binary and the unit on every run.

This removes the dependency on the timer having been installed. It also closes
the bootstrap deadlock at the delivery layer: even a device whose timer never
existed can be driven to update, because the trigger is a file write rather than
a unit that may be missing.

The unit is reconciled independently of the binary version, because the two are
versioned separately. A device can be fully current and still missing a group or
a flag that a feature depends on.

## Rejected

- **setuid on the agent binary.** Gives an unprivileged, network-facing process
  the ability to overwrite a root-owned executable.
- **A sudoers entry.** More configuration to get wrong than a six-line path unit,
  with a worse audit story.
- **Relying on the timer alone.** It is one missing unit away from silent failure
  again, which is precisely the state that was found in production.
- **Letting the daemon reach root another way.** The signal must be something an
  unprivileged process can produce without privilege escalation.

## Consequences

- Credentials for the authenticated unit fetch come from the unit itself
  (`--device-id`/`--device-token`), not the agent state file. The update service
  runs as root, where the state path resolves under a different `HOME`, so
  reading state there silently yielded nothing.
- The installer treats a missing updater template as a hard failure instead of a
  warning, and `verify_agent_readiness` regenerates a **missing** unit. Its
  earlier form only patched an existing file, so it skipped the exact case that
  was found on the device.
- A journal the agent cannot read is now a **unit repair** request, throttled to
  once an hour, because the repair restarts the service.

## What this decision is built on, and the caveat

The journal-permission diagnosis is inferred from file modes
(`-rw-r----- root:systemd-journal`) and from journald's own notice that only
`adm`/`systemd-journal` members see the full journal. **It was never confirmed by
running the collector as the agent user** (`sudo -u sync-win journalctl`), which
is the one command that would prove it. The mechanism above is sound regardless
of that; the *reason* it was needed is not yet verified.

## The lesson recorded elsewhere

The original Logs fix was signed off against rows seeded directly into the
database, which bypassed the agent. The screen looked correct while the real
pipeline had never worked. See the verification rules in `AGENTS.md`: exercise
the producer, not the table.
