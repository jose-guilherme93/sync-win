import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Device, HardwareStats } from '../../lib/types'
import Findings from './Findings.svelte'

function hardware(overrides: Partial<HardwareStats> = {}): HardwareStats {
  return {
    cpu_usage_percent: 10,
    memory_used_bytes: 50,
    memory_total_bytes: 100,
    disk_read_rate: 0,
    disk_write_rate: 0,
    uptime_seconds: 100,
    load_average: '0.1 0.1 0.1',
    power_watts: 10,
    collected_at: new Date().toISOString(),
    ...overrides
  }
}

function device(overrides: Partial<Device> = {}): Device {
  return {
    id: 'dev-1',
    hostname: 'host-1',
    user_id: 'user-1',
    status: 'online',
    last_seen_at: new Date().toISOString(),
    last_sync_at: new Date().toISOString(),
    app_count: 0,
    preference_count: 0,
    saves_count: 0,
    saves_size_bytes: 0,
    hardware: hardware({ agent_version: '0.6.2', lynis_available: true }),
    ...overrides
  }
}

describe('Findings screen', () => {
  afterEach(() => {
    cleanup()
  })

  it('shows an empty state when every device is healthy', () => {
    render(Findings, { props: { devices: [device()], onSelect: vi.fn() } })
    expect(screen.getByText('No findings')).toBeTruthy()
  })

  it('reports device-state problems that are not resource thresholds', () => {
    const stale = device({
      id: 'dev-stale',
      hostname: 'stale-host',
      last_seen_at: new Date(Date.now() - 60_000).toISOString()
    })
    const broken = device({
      id: 'dev-error',
      hostname: 'error-host',
      status: 'error',
      last_error: 'preference sync failed'
    })
    const noLynis = device({
      id: 'dev-nolynis',
      hostname: 'nolynis-host',
      hardware: hardware({ agent_version: '0.6.2', lynis_available: false })
    })
    render(Findings, { props: { devices: [broken, stale, noLynis], onSelect: vi.fn() } })

    expect(screen.getByText('Sync error', { selector: '.label-text' })).toBeTruthy()
    expect(screen.getByText('preference sync failed')).toBeTruthy()
    expect(screen.getByText('Stale', { selector: '.label-text' })).toBeTruthy()
    expect(screen.getByText('Lynis not installed', { selector: '.label-text' })).toBeTruthy()
  })

  it('flags an agent older than the newest in the fleet', () => {
    const old = device({ id: 'dev-old', hostname: 'old-host', hardware: hardware({ agent_version: '0.5.0', lynis_available: true }) })
    const fresh = device({ id: 'dev-new', hostname: 'new-host', hardware: hardware({ agent_version: '0.6.2', lynis_available: true }) })
    render(Findings, { props: { devices: [old, fresh], onSelect: vi.fn() } })

    expect(screen.getByText('Outdated agent', { selector: '.label-text' })).toBeTruthy()
    expect(screen.getByText('0.5.0 is behind 0.6.2.')).toBeTruthy()
  })

  it('selects the device when its name is clicked', async () => {
    const onSelect = vi.fn()
    const broken = device({ id: 'dev-error', hostname: 'error-host', status: 'error', last_error: 'boom' })
    render(Findings, { props: { devices: [broken], onSelect } })
    await fireEvent.click(screen.getByRole('button', { name: /error-host/ }))
    expect(onSelect).toHaveBeenCalledWith('dev-error')
  })
})
