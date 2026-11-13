import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'

import { tick } from 'svelte'

import SeriesPlot from './SeriesPlot.svelte'

const labels = [
  '2026-10-21T12:00:00Z',
  '2026-10-21T12:01:00Z',
  '2026-10-21T12:02:00Z'
]

describe('SeriesPlot', () => {
  afterEach(cleanup)

  it('shows the mountain loader while loading, with the axis rulers still visible', () => {
    render(SeriesPlot, {
      props: { labels: [], series: [], loading: true, leftMax: 100, windowMs: 3_600_000, windowEnd: Date.parse(labels[2]) }
    })

    expect(document.querySelector('[data-testid="series-loader"]')).toBeTruthy()
    // The loading area keeps its height and the ruler stays readable.
    expect(document.querySelector('.yax')?.textContent).toContain('100')
    expect(document.querySelector('.plot')).toBeTruthy()
  })

  it('shows the empty message once loading is over, without collapsing the plot', () => {
    render(SeriesPlot, {
      props: { labels: [], series: [], loading: false, emptyMessage: 'nothing to plot', height: 180 }
    })

    expect(screen.getByText('nothing to plot')).toBeTruthy()
    expect(document.querySelector('[data-testid="series-loader"]')).toBeNull()
    expect(document.querySelector('.plot')?.getAttribute('style')).toContain('180px')
  })

  it('draws one stroke and one area per series at real timestamps', () => {
    render(SeriesPlot, {
      props: {
        labels,
        series: [
          { label: 'CPU', values: [10, 20, 30], color: 'var(--chart-1)' },
          { label: 'Temp', values: [40, 50, 60], color: 'var(--chart-3)', axis: 'right', format: (v: number) => `${v}°C` }
        ],
        leftMax: 100,
        rightMax: 100,
        rightUnit: '°C'
      }
    })

    const svg = document.querySelector('.plot-svg')
    expect(svg?.getAttribute('preserveAspectRatio')).toBe('none')
    // Two series × (area + stroke).
    expect(svg?.querySelectorAll('path').length).toBe(4)
    // The newest sample of the primary series carries the head marker.
    expect(document.querySelector('.head-dot')).toBeTruthy()
    // Both rulers, right axis included.
    expect(document.querySelectorAll('.yax').length).toBe(2)
    expect(document.querySelector('.yax.r')?.textContent).toContain('100°C')
  })

  it('shows last, average, minimum and maximum for each series', () => {
    render(SeriesPlot, {
      props: {
        labels,
        series: [
          { label: 'CPU', values: [10, 20, 30], color: 'var(--chart-1)' },
          { label: 'Memory', values: [40, 50, 60], color: 'var(--chart-2)', format: (v: number) => `${v.toFixed(0)}%` }
        ]
      }
    })

    expect(screen.getByText('30')).toBeTruthy()
    // CPU has no unit format; Memory formats through its own formatter.
    expect(screen.getByText(/ø 20/)).toBeTruthy()
    expect(screen.getByText(/↓ 10/)).toBeTruthy()
    expect(screen.getByText(/ø 50%/)).toBeTruthy()
    expect(screen.getByText(/↓ 40%/)).toBeTruthy()
    expect(screen.getByText(/↑ 60%/)).toBeTruthy()
  })

  it('skips a series without values and reports the empty window', () => {
    render(SeriesPlot, {
      props: {
        labels,
        series: [{ label: 'Temp', values: [null, null, null], color: 'var(--chart-3)' }],
        emptyMessage: 'nothing to plot'
      }
    })

    expect(screen.getByText('nothing to plot')).toBeTruthy()
    expect(screen.queryByText('Temp')).toBeNull()
  })

  it('breaks the line at null values instead of bridging the gap', () => {
    render(SeriesPlot, {
      props: {
        labels,
        series: [{ label: 'CPU', values: [10, null, 30], color: 'var(--chart-1)' }],
        leftMax: 100
      }
    })

    const stroke = document.querySelector('.plot-svg path[fill="none"]')
    expect(stroke?.getAttribute('d')).toContain('M')
    // Two separate segments: the null never gets a coordinate on the line.
    expect(stroke?.getAttribute('d')?.match(/M/g)?.length).toBe(2)
  })

  // The reveal must be a frame-driven clip over data that is already plotted.
  // Appending a batch of points every few hundred milliseconds is what made the
  // first implementation look like five frames per second.
  it('reveals with a clipped frame loop, and the plotted data never grows', async () => {
    const frames: FrameRequestCallback[] = []
    vi.stubGlobal('requestAnimationFrame', (fn: FrameRequestCallback) => {
      frames.push(fn)
      return frames.length
    })
    vi.stubGlobal('cancelAnimationFrame', () => {})
    let now = 0
    vi.spyOn(performance, 'now').mockImplementation(() => now)

    render(SeriesPlot, {
      props: {
        labels,
        series: [{ label: 'CPU', values: [10, 20, 30], color: '#fff' }],
        windowMs: 3_600_000,
        windowEnd: Date.parse(labels[2]),
        revealFrom: 0.25,
        revealId: 1
      }
    })

    const svg = document.querySelector('.plot-svg') as HTMLElement
    const points = document.querySelector('[data-points]') as HTMLElement
    expect(points.dataset.points).toBe('3')
    const inset = () => Number(/([\d.]+)%/.exec(svg.style.clipPath)?.[1] ?? '0')
    // The first frame shows only the newest quarter of the window.
    expect(inset()).toBeCloseTo(75, 1)

    now = 450
    frames.shift()?.(now)
    await tick()
    // Halfway through, the covered strip is much narrower: it is animating.
    expect(inset()).toBeLessThan(20)
    expect(inset()).toBeGreaterThan(0)
    // Same three points: the reveal moved the clip, not the series.
    expect(points.dataset.points).toBe('3')

    now = 900
    frames.shift()?.(now)
    await tick()
    expect(svg.style.clipPath).toBe('')
    expect(points.dataset.points).toBe('3')
  })
})
