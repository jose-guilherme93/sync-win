// Agent health across the fleet.
//
// The device list already carries everything needed to answer "is this agent
// healthy, current, and how much does it cost": the version it runs, the state
// the updater left behind, and the agent's own CPU and RSS. That belongs in one
// place so the Settings screen, and any future summary, agree on what "stuck"
// means.
//
// The distinction that matters operationally is between "behind and will fix
// itself", "behind and the last attempt failed", and "behind and cannot ever fix
// itself". The last one is the fail-closed case: an agent built without the
// update signing key refuses every update, including the one that would give it
// a key. It reads as an ordinary failure in the journal but needs a manual
// install, so it gets its own state.

import type { Device } from './types'
import { compareVersions } from './version'
import { formatRelative } from './format'

export type AgentHealthState = 'current' | 'outdated' | 'updating' | 'failed' | 'blocked' | 'unknown'

export type AgentHealth = {
  id: string
  label: string
  state: AgentHealthState
  installed: string
  target: string
  behind: boolean
  lastAttempt: string
  error: string
  cpu: number | null
  memory: number | null
  online: boolean
  seen: string
}

const BLOCKED_MARKERS = [
  'without an update signing key',
  'refusing unsigned update',
  'invalid embedded update signing key'
]

function isBlockedError(message: string): boolean {
  const lower = message.toLowerCase()
  return BLOCKED_MARKERS.some((marker) => lower.includes(marker))
}

/**
 * True when the installed agent has proved it cannot verify any update, so the
 * only fix is installing it again. The topbar uses this to say "reinstall"
 * instead of the more optimistic "update".
 */
export function agentCannotSelfUpdate(device: Device): boolean {
  return isBlockedError(device.hardware?.agent_update_error ?? '')
}

/** True when a device's telemetry is fresh enough to trust its agent state. */
export function agentReportedRecently(device: Device): boolean {
  const seen = Date.parse(device.last_seen_at)
  if (!Number.isFinite(seen)) return false
  return Date.now() - seen <= 5 * 60_000
}

export function agentHealth(device: Device, shippedVersion = ''): AgentHealth {
  const hw = device.hardware
  const installed = hw?.agent_version?.trim() ?? ''
  const status = hw?.agent_update_status?.trim() ?? ''
  const target = hw?.agent_update_target?.trim() || shippedVersion
  const error = hw?.agent_update_error?.trim() ?? ''
  const behind =
    Boolean(installed) && Boolean(target) && compareVersions(installed, target) < 0

  let state: AgentHealthState = 'current'
  if (!installed) {
    state = 'unknown'
  } else if (status === 'failed' || error) {
    state = isBlockedError(error) ? 'blocked' : 'failed'
  } else if (behind) {
    state = status === 'requested' ? 'updating' : 'outdated'
  }

  return {
    id: device.id,
    label: device.display_name?.trim() || device.hostname || device.id,
    state,
    installed: installed || 'unknown',
    target: target || 'unknown',
    behind,
    lastAttempt: formatRelative(hw?.agent_update_at),
    error,
    cpu: typeof hw?.agent_cpu_usage === 'number' ? hw.agent_cpu_usage : null,
    memory: typeof hw?.agent_memory_bytes === 'number' ? hw.agent_memory_bytes : null,
    online: agentReportedRecently(device),
    seen: formatRelative(device.last_seen_at)
  }
}

export type AgentFleetSummary = {
  total: number
  current: number
  outdated: number
  updating: number
  failed: number
  blocked: number
  unknown: number
  needsAttention: number
  agentMemoryBytes: number
  agentCPUPercent: number
  reporting: number
}

export function summarizeAgentFleet(devices: Device[], shippedVersion = ''): AgentFleetSummary {
  const rows = devices.map((device) => agentHealth(device, shippedVersion))
  const summary: AgentFleetSummary = {
    total: rows.length,
    current: 0,
    outdated: 0,
    updating: 0,
    failed: 0,
    blocked: 0,
    unknown: 0,
    needsAttention: 0,
    agentMemoryBytes: 0,
    agentCPUPercent: 0,
    reporting: 0
  }
  for (const row of rows) {
    summary[row.state] += 1
    if (row.memory != null) {
      summary.agentMemoryBytes += row.memory
      summary.reporting += 1
    }
    if (row.cpu != null) summary.agentCPUPercent += row.cpu
  }
  summary.needsAttention = summary.failed + summary.blocked
  return summary
}

export function agentStateLabel(state: AgentHealthState): string {
  switch (state) {
    case 'current':
      return 'Up to date'
    case 'outdated':
      return 'Behind'
    case 'updating':
      return 'Update requested'
    case 'failed':
      return 'Update failed'
    case 'blocked':
      return 'Cannot self-update'
    default:
      return 'Unknown'
  }
}
