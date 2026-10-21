import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Home from './Home.svelte'

function hardware(overrides: Record<string, unknown> = {}) {
  return {
    cpu_usage_percent: 42,
    memory_used_bytes: 4 * 1024 ** 3,
    memory_total_bytes: 8 * 1024 ** 3,
    disk_partitions: [
      { mount: '/', total_bytes: 100 * 1024 ** 3, used_bytes: 55 * 1024 ** 3, free_bytes: 45 * 1024 ** 3, used_percent: 55 }
    ],
    uptime_seconds: 3600,
    load_average: '0.10',
    collected_at: '2026-10-21T12:00:00Z',
    ...overrides
  } as any
}

function device(id: string, hostname: string, overrides: Record<string, unknown> = {}) {
  return {
    id,
    hostname,
    user_id: 'user-1',
    status: 'online',
    last_seen_at: '2026-10-21T12:00:00Z',
    last_sync_at: '2026-10-21T12:00:00Z',
    app_count: 0,
    preference_count: 0,
    saves_count: 0,
    saves_size_bytes: 0,
    hardware: hardware(),
    ...overrides
  } as any
}

function renderHome(devices: any[], onSelect = vi.fn(), onAdd = vi.fn(), onAlerts = vi.fn()) {
  render(Home, { props: { devices, onSelect, onAdd, onAlerts } })
  return { onSelect, onAdd, onAlerts }
}

describe('Home fleet list', () => {
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it('shows CPU, RAM and disk inline as percentages and bars, with no chart', () => {
    renderHome([device('d1', 'alpha')])

    expect(screen.getByText('alpha')).toBeTruthy()
    expect(screen.getByText('42%')).toBeTruthy()
    expect(screen.getByText('50%')).toBeTruthy()
    expect(screen.getByText('55%')).toBeTruthy()

    const bars = document.querySelectorAll<HTMLElement>('.usage-item .usage-track i')
    expect(bars).toHaveLength(3)
    expect(bars[0].style.width).toBe('42%')
    expect(bars[1].style.width).toBe('50%')
    expect(bars[2].style.width).toBe('55%')

    // The fleet list must not carry a time-series chart; those belong to the
    // device Overview.
    expect(document.querySelector('.devices svg')).toBeNull()
  })

  it('opens the device when its row is clicked', async () => {
    const { onSelect } = renderHome([device('d1', 'alpha')])

    await fireEvent.click(screen.getByText('alpha'))
    expect(onSelect).toHaveBeenCalledWith('d1')
  })

  it('links the alert count to the Alerts screen', async () => {
    const critical = device('bad', 'burning', {
      hardware: hardware({ cpu_usage_percent: 95, cpu_temperature: 88 })
    })
    const { onAlerts } = renderHome([device('ok', 'calm'), critical])

    await fireEvent.click(screen.getByRole('button', { name: /alerts/i }))
    expect(onAlerts).toHaveBeenCalled()
  })

  it('sorts a critical device first', () => {
    const critical = device('bad', 'burning', {
      hardware: hardware({ cpu_usage_percent: 95 })
    })
    renderHome([device('ok', 'calm'), critical])

    const firstRow = screen.getByRole('list').querySelector('li')
    expect(firstRow?.textContent).toContain('burning')
  })

  it('adds a device from the summary bar', async () => {
    const { onAdd } = renderHome([])

    await fireEvent.click(screen.getByRole('button', { name: '+ Add device' }))
    expect(onAdd).toHaveBeenCalled()
  })

  it('shows the install hint when the fleet is empty', () => {
    renderHome([])

    expect(screen.getByText('No devices yet')).toBeTruthy()
  })
})
