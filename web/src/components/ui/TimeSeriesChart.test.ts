import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

// Chart.js needs a real canvas; the numbers under test are the legend
// statistics, which are plain DOM.
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
  Tooltip: class {},
  Legend: class {}
}))

import TimeSeriesChart from './TimeSeriesChart.svelte'

describe('TimeSeriesChart', () => {
  afterEach(cleanup)

  it('shows last, average, minimum and maximum for each series', () => {
    render(TimeSeriesChart, {
      props: {
        labels: ['2026-10-21T12:00:00Z', '2026-10-21T12:01:00Z', '2026-10-21T12:02:00Z'],
        series: [
          { label: 'CPU', values: [10, 20, 30], color: 'var(--accent)' },
          { label: 'Temp', values: [40, 50, 60], color: 'var(--series-3)', axis: 'right', format: (v: number) => `${v}°C` }
        ],
        leftUnit: '%',
        rightUnit: '°C'
      }
    })

    expect(screen.getByText('CPU')).toBeTruthy()
    // Current value and the period statistics, all with their own unit.
    expect(screen.getByText('30%')).toBeTruthy()
    expect(screen.getByText(/ø 20%/)).toBeTruthy()
    expect(screen.getByText(/↓ 10%/)).toBeTruthy()
    expect(screen.getByText(/↑ 30%/)).toBeTruthy()
    expect(screen.getByText('60°C')).toBeTruthy()
    expect(screen.getByText(/ø 50°C/)).toBeTruthy()
  })

  it('skips a series without values and reports the empty window', () => {
    render(TimeSeriesChart, {
      props: {
        labels: [],
        series: [
          { label: 'Temp', values: [null, null], color: 'var(--series-3)' },
          { label: 'CPU', values: [], color: 'var(--accent)' }
        ],
        emptyMessage: 'nothing to plot'
      }
    })

    expect(screen.getByText('nothing to plot')).toBeTruthy()
    expect(screen.queryByText('Temp')).toBeNull()
    expect(screen.queryByText('CPU')).toBeNull()
  })
})
