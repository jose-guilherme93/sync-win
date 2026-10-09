# Decision: the installer fetches unit templates, and units name only real groups

## Two failures, one install

An Arch machine failed to install the agent. The transcript showed three things
at once: the service installed but never started, all three updater templates
reported missing, and the whole install rolled back.

## 1. The templates were never where the installer looked

`SYNCWIN_UPDATE_SERVICE_TEMPLATE="/app/sync-win-agent-update.service"`. `/app` is
a path **inside the server container**. The installer runs on the device, so that
file has never existed on any machine. The check always failed, and the code
answered with `warn … return 0`.

Consequence: **automatic updates were never enabled on any device.** Not a
regression — a path that never worked, hidden behind a warning nobody read. It is
also why `sync-win-agent-update.timer` was `not-found` on the machine inspected
while debugging the Logs screen, which had been attributed to a failed install
step rather than a code defect.

## 2. The hard failure that followed was worse than the silence

Making that warning fatal looked right: an agent with no update path is
unrecoverable without a human. But the installer has `trap rollback EXIT`, so the
non-zero return **rolled back the entire installation** — the agent was installed
and then removed because automatic updates were unavailable.

Losing automatic updates is a degraded outcome. Refusing to install the agent is
a broken one. The fix keeps them separate:

- essential steps stay fatal
- a failed template fetch warns and continues

## 3. A unit naming a group that does not exist will not start

The unit was written with a fixed `SupplementaryGroups=docker systemd-journal`.
systemd **refuses to start** a unit that names a group the host does not have, so
on a machine without Docker the service installed and never came up, reported
only as "Service installed but not running yet". `add_docker_group` already
warned that the group was missing; the unit was not told.

Groups are now computed from what exists: `docker` only when present, and
`systemd-journal` with `adm` as the fallback, because journal files are
`0640 root:systemd-journal` and the agent needs one of the two to read its own
logs.

## The decision

`GET /api/agent/units?name=` serves the templates. A map keyed by short name, not
a joined path, so an unknown name, a bare filename and a traversal attempt all
return 404 rather than reading an arbitrary file. Overridable through
`SYNCWIN_UNIT_DIR` so the handler is testable without a container filesystem,
matching the existing `SYNCWIN_AGENT_BINARY`.

The installer fetches them in the same step as the binary, validating that each
response starts with `[Unit]`.

## What this cost, and the lesson

The hard-failure change was mine, made without checking where the templates came
from — despite having described `/app` as a server-side path in an earlier turn.
It broke the install of a real user.

The `AGENTS.md` rule "do not swallow errors that matter" was already there. What
was missing is its companion, now recorded: **understand where a value comes from
before changing how you react to its absence.** The warning was a symptom; the
wrong path was the disease. Treating the symptom made things worse.

## Verification

The endpoint was exercised against a real server: four valid names returned
`[Unit]`, and the empty name, `/etc/passwd`, `../../etc/passwd`, a bare filename,
`AGENT` and a NUL-suffixed name all returned 404. `POST` returned 405. The
installer's real `download_unit_templates` function, extracted rather than
reimplemented, fetched 4/4 from that server and filled in every variable; against
an unreachable server it warned and exited 0.

Not verified: the installer end to end on a real host with systemd, and
`refreshUnitFile` / the path unit, which still need root and a real systemd.
