import { cleanup, render, screen, within } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Agents from './Agents.svelte'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

const now = new Date().toISOString()
const GB = 1024 ** 3

function device(id: string, hostname: string, hardware: Record<string, unknown>) {
  return {
    id,
    hostname,
    user_id: 'owner',
    status: 'online',
    last_seen_at: now,
    last_sync_at: now,
    app_count: 0,
    preference_count: 0,
    saves_count: 0,
    saves_size_bytes: 0,
    hardware: {
      cpu_usage_percent: 10,
      memory_used_bytes: 4 * GB,
      memory_total_bytes: 16 * GB,
      disk_read_rate: 0,
      disk_write_rate: 0,
      uptime_seconds: 3600,
      load_average: '0.1',
      power_watts: 0,
      collected_at: now,
      ...hardware
    }
  }
}

function stubFetch(options: { signature?: number; usage?: unknown } = {}) {
  const { signature = 200, usage = { devices: {} } } = options
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/api/agents/usage')) return json(usage)
      if (url.includes('/api/agent/version')) return json({ version: '0.12.0' })
      if (url.includes('/api/agent/signature')) {
        return signature === 200 ? new Response('c2ln', { status: 200 }) : json({}, signature)
      }
      return json([])
    })
  )
}

describe('Agents screen', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('states the agent share of the machine RAM, not just the megabytes', async () => {
    stubFetch()
    render(Agents, {
      props: {
        shippedAgentVersion: '0.12.0',
        devices: [
          device('dev-a', 'box', {
            agent_version: '0.12.0',
            agent_memory_bytes: 12_000_000,
            agent_cpu_usage: 0.41,
            cpu_core_usage: [1, 2, 3, 4, 5, 6, 7, 8]
          })
        ] as never
      }
    })

    const row = (await screen.findByText('box')).closest('article') as HTMLElement
    expect(within(row).getByText(/11\.4 MB · \d\.\d+%/)).toBeTruthy()
    expect(within(row).getByText('0.41%')).toBeTruthy()
    expect(within(row).getByText(/share of all 8 cores/)).toBeTruthy()
    expect(within(row).getByText(/of 16(\.0)? GB total/)).toBeTruthy()
  })

  it('says the machine RAM is unknown instead of drawing a zero share', async () => {
    stubFetch()
    render(Agents, {
      props: {
        devices: [device('dev-a', 'box', { agent_memory_bytes: 12_000_000, memory_total_bytes: 0 })] as never
      }
    })

    const row = (await screen.findByText('box')).closest('article') as HTMLElement
    expect(within(row).getByText('machine RAM not reported')).toBeTruthy()
  })

  it('draws the two-hour history when the series exists and says so when it does not', async () => {
    stubFetch({
      usage: {
        devices: {
          'dev-a': [
            { t: now, rss: 11_000_000, cpu: 0.3 },
            { t: now, rss: 12_000_000, cpu: 0.4 },
            { t: now, rss: 11_500_000, cpu: 0.35 }
          ]
        }
      }
    })
    render(Agents, {
      props: {
        devices: [
          device('dev-a', 'with-history', { agent_version: '0.12.0', agent_memory_bytes: 12_000_000 }),
          device('dev-b', 'without-history', { agent_version: '0.12.0', agent_memory_bytes: 9_000_000 })
        ] as never
      }
    })

    const withHistory = (await screen.findByText('with-history')).closest('article') as HTMLElement
    await vi.waitFor(() => expect(withHistory.querySelector('polyline')).toBeTruthy())
    const without = screen.getByText('without-history').closest('article') as HTMLElement
    expect(within(without).getByText('no history')).toBeTruthy()
  })

  it('names a blocked agent and the reason on its own row', async () => {
    stubFetch()
    render(Agents, {
      props: {
        devices: [
          device('dev-blocked', 'frozen-box', {
            agent_version: '0.10.0',
            agent_update_status: 'failed',
            agent_update_error: 'agent was built without an update signing key'
          })
        ] as never
      }
    })

    const row = (await screen.findByText('frozen-box')).closest('article') as HTMLElement
    expect(within(row).getByText(/Cannot self-update/)).toBeTruthy()
    expect(within(row).getByText(/built without an update signing key/)).toBeTruthy()
    expect(screen.getByText(/cannot self-update: the installed/)).toBeTruthy()
  })

  it('shows the server version and warns when no signature is served', async () => {
    stubFetch({ signature: 404 })
    render(Agents, {
      props: {
        devices: [device('dev-a', 'box', { agent_version: '0.12.0' })] as never
      }
    })

    expect(await screen.findByText('0.12.0')).toBeTruthy()
    expect(await screen.findByText('missing')).toBeTruthy()
    expect(screen.getByText(/no agent signature, so no device can update/)).toBeTruthy()
  })

  it('summarises the fleet state above the rows', async () => {
    stubFetch()
    render(Agents, {
      props: {
        shippedAgentVersion: '0.12.0',
        devices: [
          device('dev-a', 'a', { agent_version: '0.12.0', agent_update_status: 'current', agent_memory_bytes: 10_000_000 }),
          device('dev-b', 'b', { agent_version: '0.10.0', agent_memory_bytes: 20_000_000, memory_total_bytes: 0 })
        ] as never
      }
    })

    expect(await screen.findByText('2')).toBeTruthy()
    expect(screen.getByText('1 current')).toBeTruthy()
    expect(screen.getByText('1 behind')).toBeTruthy()
  })

  it('says so when there is no device instead of an empty list', async () => {
    stubFetch()
    render(Agents)
    expect(await screen.findByText('No devices enrolled yet.')).toBeTruthy()
  })
})
