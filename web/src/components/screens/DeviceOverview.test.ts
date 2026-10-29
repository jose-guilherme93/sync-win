import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// Chart.js needs a real canvas, and the assertions below are about the numbers
// around the chart (legend statistics, section order).
vi.mock('chart.js', () => ({
  Chart: class {
    static register() {}
    constructor() {}
    update() {}
    destroy() {}
  },
  LineController: class {},
  LineElement: class {},
  PointElement: class {},
  LinearScale: class {},
  CategoryScale: class {},
  Filler: class {},
  Tooltip: class {},
  Legend: class {}
}))

import DeviceOverview from './DeviceOverview.svelte'

const device = {
  id: 'dev-1',
  hostname: 'workstation',
  user_id: 'user-1',
  status: 'online',
  last_seen_at: '2026-10-21T12:00:00Z',
  last_sync_at: '2026-10-21T12:00:00Z',
  app_count: 0,
  preference_count: 0,
  saves_count: 0,
  saves_size_bytes: 0,
  hardware: {
    cpu_usage_percent: 21,
    cpu_temperature: 0,
    cpu_clock_mhz: 3410,
    memory_used_bytes: 2 * 1024 ** 3,
    memory_total_bytes: 8 * 1024 ** 3,
    swap_total_bytes: 0,
    cpu_core_usage: [10, 20],
    load_average: '0.50 0.40 0.30',
    uptime_seconds: 5000,
    collected_at: '2026-10-21T12:00:00Z',
    agent_version: '0.9.0',
    operating_system: 'Linux',
    kernel_version: '6.8.0',
    architecture: 'amd64',
    disk_partitions: [
      { mount: '/', device: '/dev/sda1', total_bytes: 100 * 1024 ** 3, used_bytes: 55 * 1024 ** 3, free_bytes: 45 * 1024 ** 3, used_percent: 55 }
    ],
    network_ifaces: [],
    top_cpu_processes: [],
    top_mem_processes: []
  }
}

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

describe('DeviceOverview', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn(async () => json({
      points: [
        { timestamp: '2026-10-21T12:00:00Z', cpu_avg: 10, mem_avg: 40, temp_avg: 0, net_rx_avg: 100, net_tx_avg: 50 },
        { timestamp: '2026-10-21T12:01:00Z', cpu_avg: 20, mem_avg: 50, temp_avg: 0, net_rx_avg: 200, net_tx_avg: 80 },
        { timestamp: '2026-10-21T12:02:00Z', cpu_avg: 30, mem_avg: 60, temp_avg: 0, net_rx_avg: 300, net_tx_avg: 90 }
      ]
    })))
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('starts at the Performance chart without repeating the headline metrics', async () => {
    render(DeviceOverview, { props: { device: device as any } })

    expect(await screen.findByRole('heading', { name: 'Performance' })).toBeTruthy()
    expect(await screen.findByText(/ø 20%/)).toBeTruthy()
    // CPU, memory and disk live in the chart and its legend, not in a second
    // row of cards above it.
    expect(document.querySelector('[data-vital]')).toBeNull()
  })

  it('treats a missing temperature sensor as absent, not as 0°C', async () => {
    render(DeviceOverview, { props: { device: device as any } })

    const toggle = screen.getByRole('button', { name: 'Temp' }) as HTMLButtonElement
    expect(toggle.disabled).toBe(true)
  })

  it('puts period numbers in the chart legend', async () => {
    render(DeviceOverview, { props: { device: device as any } })

    expect(await screen.findByText(/ø 20%/)).toBeTruthy()
    expect(screen.getByText(/↓ 10%/)).toBeTruthy()
    expect(screen.getByText(/↑ 30%/)).toBeTruthy()
  })
})
