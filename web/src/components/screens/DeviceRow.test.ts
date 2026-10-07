import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'
import { addTelemetryPoint, resetTelemetry } from '../../lib/telemetry-store'
import DeviceRow from './DeviceRow.svelte'

describe('DeviceRow sparklines', () => {
  afterEach(() => {
    cleanup()
    resetTelemetry()
  })

  it('labels the CPU trend so the graph is not anonymous', () => {
    addTelemetryPoint('dev-1', { collected_at: '2026-10-07T12:00:00Z', cpu_usage_percent: 10 })
    render(DeviceRow, { props: { deviceId: 'dev-1', metric: 'cpu' } })

    expect(screen.getByText('CPU trend')).toBeTruthy()
    expect(screen.getByRole('img').getAttribute('aria-label')).toContain('CPU trend')
  })

  it('labels the memory trend distinctly from the CPU one', () => {
    addTelemetryPoint('dev-1', { collected_at: '2026-10-07T12:00:00Z', cpu_usage_percent: 10, memory_used_bytes: 5, memory_total_bytes: 10 })
    render(DeviceRow, { props: { deviceId: 'dev-1', metric: 'memory' } })

    expect(screen.getByText('RAM trend')).toBeTruthy()
    expect(screen.queryByText('CPU trend')).toBeNull()
  })
})
