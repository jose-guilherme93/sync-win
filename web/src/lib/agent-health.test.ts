import { describe, expect, it } from 'vitest'
import { agentHealth, agentStateLabel, summarizeAgentFleet, agentReportedRecently, agentCannotSelfUpdate } from './agent-health'
import type { Device } from './types'

function device(overrides: Partial<Device> = {}): Device {
  return {
    id: 'dev-1',
    hostname: 'box',
    user_id: 'owner',
    status: 'online',
    last_seen_at: new Date().toISOString(),
    last_sync_at: new Date().toISOString(),
    app_count: 0,
    preference_count: 0,
    saves_count: 0,
    saves_size_bytes: 0,
    ...overrides
  }
}

function withHardware(overrides: Record<string, unknown>, deviceOverrides: Partial<Device> = {}): Device {
  return device({ hardware: { cpu_usage_percent: 1, memory_used_bytes: 1, memory_total_bytes: 2, disk_read_rate: 0, disk_write_rate: 0, uptime_seconds: 1, load_average: '0', power_watts: 0, collected_at: '', ...overrides } as Device['hardware'], ...deviceOverrides })
}

describe('agentHealth', () => {
  it('reports current when the device matches the shipped version', () => {
    const row = agentHealth(withHardware({ agent_version: '0.11.3' }), '0.11.3')
    expect(row.state).toBe('current')
    expect(row.behind).toBe(false)
  })

  it('reports behind when the installed agent is older than the server', () => {
    const row = agentHealth(withHardware({ agent_version: '0.10.0' }), '0.11.3')
    expect(row.state).toBe('outdated')
    expect(row.behind).toBe(true)
    expect(row.target).toBe('0.11.3')
  })

  it('distinguishes an unsigned agent that can never update itself', () => {
    const row = agentHealth(
      withHardware({
        agent_version: '0.10.0',
        agent_update_status: 'failed',
        agent_update_error: 'agent was built without an update signing key; refusing unsigned update'
      }),
      '0.11.3'
    )
    expect(row.state).toBe('blocked')
  })

  it('treats any other failure as an ordinary update failure', () => {
    const row = agentHealth(
      withHardware({
        agent_version: '0.10.0',
        agent_update_status: 'failed',
        agent_update_error: 'signature endpoint returned 404'
      }),
      '0.11.3'
    )
    expect(row.state).toBe('failed')
  })

  it('shows a requested update as in progress, not failed', () => {
    const row = agentHealth(
      withHardware({ agent_version: '0.10.0', agent_update_status: 'requested', agent_update_target: '0.11.3' }),
      '0.11.3'
    )
    expect(row.state).toBe('updating')
  })

  it('does not claim a device is current with no agent version at all', () => {
    const row = agentHealth(withHardware({}), '0.11.3')
    expect(row.state).toBe('unknown')
    expect(row.installed).toBe('unknown')
  })

  it('uses the update target when the device reported one', () => {
    const row = agentHealth(
      withHardware({ agent_version: '0.10.0', agent_update_target: '0.11.2' }),
      '0.11.3'
    )
    expect(row.target).toBe('0.11.2')
  })
})

describe('summarizeAgentFleet', () => {
  it('counts states and adds up the agent memory that was reported', () => {
    const fleet = summarizeAgentFleet(
      [
        withHardware({ agent_version: '0.11.3', agent_memory_bytes: 10_000_000 }),
        withHardware({ agent_version: '0.10.0', agent_memory_bytes: 5_000_000 }),
        withHardware({
          agent_version: '0.10.0',
          agent_update_status: 'failed',
          agent_update_error: 'agent was built without an update signing key'
        })
      ],
      '0.11.3'
    )
    expect(fleet.total).toBe(3)
    expect(fleet.current).toBe(1)
    expect(fleet.outdated).toBe(1)
    expect(fleet.blocked).toBe(1)
    expect(fleet.needsAttention).toBe(1)
    expect(fleet.agentMemoryBytes).toBe(15_000_000)
    expect(fleet.reporting).toBe(2)
  })
})

describe('agentReportedRecently', () => {
  it('treats a stale timestamp as not reporting', () => {
    const old = device({ last_seen_at: new Date(Date.now() - 60 * 60_000).toISOString() })
    expect(agentReportedRecently(old)).toBe(false)
  })
})

describe('agentCannotSelfUpdate', () => {
  it('recognises the fail-closed error so the header can say reinstall', () => {
    expect(
      agentCannotSelfUpdate(
        withHardware({ agent_update_error: 'agent was built without an update signing key; refusing unsigned update' })
      )
    ).toBe(true)
    expect(agentCannotSelfUpdate(withHardware({ agent_update_error: 'signature endpoint returned 404' }))).toBe(false)
    expect(agentCannotSelfUpdate(withHardware({}))).toBe(false)
  })
})

describe('agentStateLabel', () => {
  it('names the fail-closed case in words an operator can act on', () => {
    expect(agentStateLabel('blocked')).toBe('Cannot self-update')
  })
})
