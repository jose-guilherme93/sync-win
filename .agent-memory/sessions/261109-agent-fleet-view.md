---
id: sessions/261109-agent-fleet-view
type: session
title: "Agent fleet view in Administration → Settings"
description: >-
  The dashboard could say a device's agent was behind, but not why it stayed
  behind. The updater is a root systemd unit with no channel back, so a
  fail-closed agent looked identical to one that was simply out of date. The
  updater now records its outcome in a file next to the agent state, the daemon
  reports it with telemetry, and Settings shows the whole fleet: version against
  the server's, last attempt and its reason, and the agent's own CPU and memory.
tags: [agent, updates, dashboard, telemetry, contract, settings]
source: agent
created: 2026-11-09
updated: 2026-11-09
status: active
---

## What changed

- **The updater records its outcome.** `updateAgentAtIdentity` wraps
  `performAgentUpdate` and writes `agent-update-state.json` in the state
  directory: `requested | current | updated | failed`, the versions involved, the
  error text and a timestamp. A separate file from `agent-state.json` on purpose
  — the daemon owns that one and rewrites it every cycle, and the updater runs as
  root, which would leave a file the service user could no longer replace.
  The path is resolved in this order: `STATE_DIRECTORY`, `XDG_STATE_HOME`, then
  `/var/lib/sync-win` when it exists. The last step matters: the update unit on
  older devices does not declare `StateDirectory`, so root's own state lookup
  would land in `/root/.local/state` and the dashboard would read nothing. Adding
  `StateDirectory=sync-win` to that unit instead was rejected because systemd
  re-chowns an existing directory to the unit's user, which is root there — it
  would take `/var/lib/sync-win` away from the agent.
- **The daemon reports it.** `applyUpdateStateToStats` copies the file into the
  telemetry payload as `agent_update_status`, `agent_update_target`,
  `agent_update_error` and `agent_update_at`. Absent means the updater never ran
  here, which the dashboard treats as `unknown`, never as a failure.
- **Settings gained an Agent fleet panel** (`web/src/lib/agent-health.ts` plus a
  card at the top of `AppSettings.svelte`): per device the installed version
  against the one the server ships, the update state, the last attempt, and the
  agent's CPU and memory. The summary counts current, behind and
  needs-attention, and totals the fleet's agent RSS and CPU.
- **`Cannot self-update` is its own state.** The fail-closed error ("agent was
  built without an update signing key") is recognised by text and shown
  separately from an ordinary failure, because the fix is a manual install, not
  another retry. The device header now says `· reinstall` instead of `· update`
  for exactly that case.
- **Agent 0.12.0, contract 1.10.0.** The new telemetry fields are part of the
  contract; a device on an older agent reports nothing and reads as `unknown`.

## Why

The production incident: a device on agent 0.10.0 retried every 15 minutes and
every 5 minutes from the path unit, failed on the signature check and looked
healthy everywhere in the dashboard. Nothing told the operator that the device
could never fix itself. Reporting the updater's own verdict closes that gap
without giving the dashboard a way to run anything on a device.

## Not done

- No global on/off switch for automatic updates. The agent still decides from its
  own timer and path unit; a server-side policy would need a fetch on the
  agent's cycle and a stored setting.
- The device's own Settings tab does not repeat this panel; the fleet view is
  deliberately in Administration → Settings.
