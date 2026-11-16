import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// The overview draws SVG, not canvas: the assertions below are about the
// loader, the recent-first fill and the delta merge, all plain DOM.
import DeviceOverview from './DeviceOverview.svelte'
import { recentWindowCount } from '../../lib/telemetry-series'
import { DEFAULT_RANGE, selectedRange } from '../../lib/timerange'
import { addTelemetryPoint, resetTelemetry } from '../../lib/telemetry-store'

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
    net_rx_rate: 1_200_000,
    net_tx_rate: 240_000,
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

// One hour of one-minute samples in the series contract shape.
const T0 = 1_769_000_000 // unix seconds
const TOTAL = 60

function contractPayload(count: number, extraNewest = 0) {
  const to = T0 + 3600 + extraNewest * 60
  const points = Array.from({ length: count }, (_, index) => {
    const at = to - (count - 1 - index) * 60
    return { t: at, cpu: 10 + (index % 7), mem: 40 + (index % 5), rx: 1000, tx: 500, temp: 0, disk: 55 }
  })
  return { window: '1h', resolution: 'raw', from: to - 3600, to, points }
}

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), { status: 200, headers: { 'Content-Type': 'application/json' } })
}

function plottedPoints(): number {
  const chart = document.querySelector('[data-points]')
  return chart ? Number((chart as HTMLElement).dataset.points) : -1
}

describe('DeviceOverview', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })

  afterEach(() => {
    cleanup()
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('shows the mountain loader while no series has arrived, and the axis under it', async () => {
    let respond: ((response: Response) => void) | undefined
    vi.stubGlobal('fetch', vi.fn(() => new Promise<Response>((resolve) => (respond = resolve))))

    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)

    expect(screen.getByRole('heading', { name: 'Performance' })).toBeTruthy()
    // Loading state is the mountain stroke over a visible axis, never a
    // skeleton, and the plot keeps its height.
    expect(document.querySelector('[data-testid="series-loader"]')).toBeTruthy()
    expect(document.querySelector('.yax')).toBeTruthy()

    respond?.(json(contractPayload(TOTAL)))
    await vi.advanceTimersByTimeAsync(0)

    expect(document.querySelector('[data-testid="series-loader"]')).toBeNull()
  })

  // The whole window arrives in one response and is plotted in one go; the
  // reveal is a clip over data that is already there, never a slice that grows
  // a batch at a time (that ran at ~4 fps and looked like dropped frames).
  it('plots the whole window from one request and reveals it without a second download', async () => {
    const fetchMock = vi.fn(async (_url: RequestInfo | URL) => json(contractPayload(TOTAL)))
    vi.stubGlobal('fetch', fetchMock)

    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)

    expect(plottedPoints()).toBe(TOTAL)
    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(String(fetchMock.mock.calls[0][0])).toContain('window=1h')
    expect(String(fetchMock.mock.calls[0][0])).not.toContain('since=')

    // A poll appends; it must not re-request the window or rebuild the series.
    await vi.advanceTimersByTimeAsync(30_000)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(String(fetchMock.mock.calls[1][0])).toContain('since=')
  })

  it('the reveal never replays while the range stays the same', async () => {
    let now = T0
    const fetchMock = vi.fn(async (_url: RequestInfo | URL) => {
      // Each poll adds two samples at the tail.
      now += 20
      return json(contractPayload(TOTAL + Math.round((now - T0) / 10)))
    })
    vi.stubGlobal('fetch', fetchMock)

    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)
    const first = plottedPoints()

    await vi.advanceTimersByTimeAsync(30_000)
    // The series only grew at the tail; it was not reset to the recent window.
    expect(plottedPoints()).toBeGreaterThan(first)
  })

  it('reduced motion skips the reveal entirely', async () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true, media: '', addEventListener() {}, removeEventListener() {}, addListener() {}, removeListener() {}, dispatchEvent: () => false, onchange: null } as any)
    vi.stubGlobal('fetch', vi.fn(async () => json(contractPayload(TOTAL))))

    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)

    expect(plottedPoints()).toBe(TOTAL)
    const svg = document.querySelector('.plot-svg') as HTMLElement | null
    expect(svg?.style.clipPath ?? '').toBe('')
  })

  it('a since delta appends the new tail without duplicating points already on the chart', async () => {
    const fetchMock = vi.fn(async (_url: RequestInfo | URL) => json(contractPayload(TOTAL + 1)))
    // The first answer is the full window; the delta response carries the
    // whole window again (the history-v2 bridge cannot honour since), so the
    // merge must keep every timestamp once.
    fetchMock.mockResolvedValueOnce(json(contractPayload(TOTAL)))
    vi.stubGlobal('fetch', fetchMock)

    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)
    expect(plottedPoints()).toBe(TOTAL)

    // The 30 s refetch asks only for the delta.
    await vi.advanceTimersByTimeAsync(30_000)
    expect(fetchMock).toHaveBeenCalledTimes(2)
    const deltaURL = String(fetchMock.mock.calls[1][0])
    expect(deltaURL).toContain('since=')
    // The 30 s refetch asks only for the delta: since the newest plotted point.
    expect(deltaURL).toContain(`since=${T0 + 3600}`)
    expect(plottedPoints()).toBe(TOTAL + 1)
  })

  it('renders the vitals strip above the charts', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json(contractPayload(TOTAL))))
    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)

    const vitals = document.querySelector('[data-testid="vitals"]')
    expect(vitals).toBeTruthy()
    const labels = Array.from(vitals?.querySelectorAll('.v-label') ?? []).map((el) => el.textContent)
    expect(labels).toEqual(['CPU', 'Memory', 'Disk /', 'Temp', 'Load', 'Network'])
    const cpu = vitals?.querySelector('.v-num')
    expect(cpu?.textContent).toBe('21%')
    // Memory vital carries used / total, not a second CPU card.
    expect(vitals?.textContent).toContain('2.0 GB / 8.0 GB')
  })

  it('treats a missing temperature sensor as absent, not as 0°C', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json(contractPayload(TOTAL))))
    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)

    const toggle = screen.getByRole('button', { name: 'Temp' }) as HTMLButtonElement
    expect(toggle.disabled).toBe(true)
    const tempVital = document.querySelectorAll('[data-testid="vitals"] .vital')[3]
    expect(tempVital?.querySelector('.v-num')?.textContent).toBe('—°C')
  })

  it('puts period numbers in the chart legend', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => json(contractPayload(TOTAL))))
    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)

    expect(screen.getAllByText('CPU').length).toBeGreaterThan(0)
    // Values in the payload repeat 10..16, so the statistics are exact.
    expect(screen.getByText(/↓ 10%/)).toBeTruthy()
    expect(screen.getByText(/↑ 16%/)).toBeTruthy()
    expect(screen.getByText(/ø 1[0-6]%/)).toBeTruthy()
  })
})

// Live mode: a seeded one-minute window, a 1 s single-device poll, a server
// lease that accelerates the agent, and a hard 15-minute ceiling.
describe('DeviceOverview Live', () => {
  type Call = { url: string; init?: RequestInit }

  // A fetch router: the lease endpoint, the series endpoint (for the seed),
  // the single-device telemetry poll and — loudly — the fleet endpoint, which
  // must never be called while Live is open.
  function liveFetchMock(options: { series?: unknown; telemetry?: unknown | (() => unknown); leaseStatus?: number } = {}) {
    const calls: Call[] = []
    const mock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input).replace(window.location.origin, '')
      calls.push({ url, init })
      if (url.includes('/live')) {
        if (options.leaseStatus) return new Response('lease unavailable', { status: options.leaseStatus })
        return json({ active: JSON.parse(String(init?.body ?? '{}')).active, until: Math.floor(Date.now() / 1000) + 60 })
      }
      if (url.endsWith('/detail')) {
        const hw = typeof options.telemetry === 'function' ? options.telemetry() : (options.telemetry ?? liveHardware(28))
        return json({ id: 'dev-1', hostname: 'workstation', hardware: hw })
      }
      if (url.includes('/telemetry/series')) {
        // A delta poll asks for `since=`; answer with the next sample so the
        // live window grows exactly like the real endpoint.
        if (url.includes('since=')) {
          const at = Math.floor(Date.now() / 1000)
          return json({
            window: '30m', resolution: 'raw', from: at - 1800, to: at,
            points: [{ t: at, cpu: 25, mem: 41, rx: 1000, tx: 500, temp: 0, disk: 55 }]
          })
        }
        return json(options.series ?? contractPayload(TOTAL))
      }
      if (url.endsWith('/telemetry')) {
        const value = typeof options.telemetry === 'function' ? options.telemetry() : options.telemetry
        return json(value ?? {})
      }
      throw new Error('unexpected fetch while Live: ' + url)
    })
    vi.stubGlobal('fetch', mock)
    return calls
  }

  function liveHardware(cpu: number, ageMs = 0) {
    return {
      ...device.hardware,
      cpu_usage_percent: cpu,
      collected_at: new Date(Date.now() - ageMs).toISOString()
    }
  }

  function leaseBodies(calls: Call[]): boolean[] {
    return calls
      .filter((call) => call.url.includes('/live'))
      .map((call) => JSON.parse(String(call.init?.body ?? '{}')).active)
  }

  // The live poll reads the single-device detail, not the fleet and not the
  // agent's upload endpoint.
  function detailCalls(calls: Call[]): number {
    return calls.filter((call) => call.url.endsWith('/detail')).length
  }

  // A series window whose points sit in the last minute of the fake clock, so
  // the seed lands inside the live window instead of in history.
  function recentSeriesPayload(count: number) {
    const nowSec = Date.now() / 1000
    const points = Array.from({ length: count }, (_, index) => ({
      t: nowSec - (count - index) * 10,
      cpu: 20 + index, mem: 40, rx: 1000, tx: 500, temp: 0, disk: 55
    }))
    return { window: '30m', resolution: 'raw', from: nowSec - 1800, to: nowSec, points }
  }

  beforeEach(() => {
    vi.useFakeTimers()
    selectedRange.set(DEFAULT_RANGE)
  })

  afterEach(() => {
    cleanup()
    selectedRange.set(DEFAULT_RANGE)
    resetTelemetry()
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('entering Live seeds the window from memory and never shows the loader', async () => {
    // The fleet poll already collected two samples for this device.
    addTelemetryPoint('dev-1', liveHardware(30, 10_000))
    addTelemetryPoint('dev-1', liveHardware(40, 5_000))

    const calls = liveFetchMock({ telemetry: liveHardware(50) })
    selectedRange.set('live')
    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)

    // The chart opens with what is already in memory: no loader, no waiting.
    expect(document.querySelector('[data-testid="series-loader"]')).toBeNull()
    expect(plottedPoints()).toBe(2)
    // The seed came from the store, so no series download was needed.
    expect(calls.filter((call) => call.url.includes('/telemetry/series')).length).toBe(0)
  })

  it('with an empty store it seeds the window from the tail of the 30m series', async () => {
    const calls = liveFetchMock({
      series: recentSeriesPayload(6),
      telemetry: liveHardware(50)
    })
    selectedRange.set('live')
    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)

    expect(calls.some((call) => call.url.includes('/telemetry/series?window=30m'))).toBe(true)
    expect(document.querySelector('[data-testid="series-loader"]')).toBeNull()
    expect(plottedPoints()).toBe(6)
  })

  it('polls the single device every second while Live and never downloads the fleet', async () => {
    const calls = liveFetchMock({ series: recentSeriesPayload(6), telemetry: liveHardware(21) })
    selectedRange.set('live')
    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)
    const afterMount = detailCalls(calls)

    await vi.advanceTimersByTimeAsync(3_000)
    // One small detail request per second, and the line grows at the right edge.
    expect(detailCalls(calls)).toBeGreaterThan(afterMount + 1)
    expect(plottedPoints()).toBeGreaterThan(2)
    // The fleet endpoint (/api/devices with no id) is never touched.
    expect(calls.some((call) => /\/api\/devices\/?$/.test(call.url))).toBe(false)
  })

  it('keeps the vitals strip live, not just the chart', async () => {
    let cpu = 20
    const calls = liveFetchMock({ series: recentSeriesPayload(6), telemetry: () => liveHardware(cpu) })
    selectedRange.set('live')
    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)
    const vitals = document.querySelector('[data-testid="vitals"]') as HTMLElement
    const first = vitals.querySelector('.v-num')?.textContent

    cpu = 74
    await vi.advanceTimersByTimeAsync(1_500)
    expect(detailCalls(calls)).toBeGreaterThan(0)
    expect(vitals.querySelector('.v-num')?.textContent).not.toBe(first)
    expect(vitals.querySelector('.v-num')?.textContent).toBe('74%')
  })

  it('after 15 minutes Live returns to 1h, ends the lease and stops the poll', async () => {
    let current: string | null = null
    const unsub = selectedRange.subscribe((value) => (current = value))
    const calls = liveFetchMock({ series: recentSeriesPayload(6), telemetry: liveHardware(21) })

    selectedRange.set('live')
    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(0)

    // Entering Live opens the lease; renewals keep it open while it lasts.
    expect(leaseBodies(calls)[0]).toBe(true)
    await vi.advanceTimersByTimeAsync(45_000)
    expect(leaseBodies(calls).filter((active) => active).length).toBeGreaterThan(1)

    // Jump to the ceiling instead of running 900 one-second ticks: the next
    // tick sees the elapsed time and ends Live.
    vi.setSystemTime(Date.now() + 15 * 60_000)
    await vi.advanceTimersByTimeAsync(1_000)
    expect(leaseBodies(calls).at(-1)).toBe(false)
    expect(current).toBe('1h')

    // The poll is over: nothing new arrives after the ceiling.
    const before = detailCalls(calls)
    await vi.advanceTimersByTimeAsync(5_000)
    expect(detailCalls(calls)).toBe(before)
    unsub()
  })

  it('unmounting the screen ends the lease and stops the poll', async () => {
    let current: string | null = null
    const unsub = selectedRange.subscribe((value) => (current = value))
    const calls = liveFetchMock({ telemetry: liveHardware(21) })

    selectedRange.set('live')
    const { unmount } = render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(2_000)
    expect(leaseBodies(calls)).toEqual([true])

    unmount()
    await vi.advanceTimersByTimeAsync(0)
    expect(leaseBodies(calls)).toEqual([true, false])
    expect(current).toBe('1h')

    const before = detailCalls(calls)
    await vi.advanceTimersByTimeAsync(3_000)
    expect(detailCalls(calls)).toBe(before)
    unsub()
  })

  it('a failing lease endpoint does not break Live', async () => {
    const calls = liveFetchMock({ leaseStatus: 503, telemetry: liveHardware(33) })
    selectedRange.set('live')
    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(2_000)

    // The lease failed; Live still draws, polls and shows no error.
    expect(detailCalls(calls)).toBeGreaterThan(0)
    expect(document.querySelector('[data-testid="series-loader"]')).toBeNull()
    expect(screen.queryByRole('alert')).toBeNull()
    expect(plottedPoints()).toBeGreaterThan(0)
  })

  it('hiding the tab ends the lease and returns to 1h', async () => {
    let current: string | null = null
    const unsub = selectedRange.subscribe((value) => (current = value))
    const calls = liveFetchMock({ telemetry: liveHardware(21) })

    selectedRange.set('live')
    render(DeviceOverview, { props: { device: device as any } })
    await vi.advanceTimersByTimeAsync(1_000)
    expect(leaseBodies(calls)).toEqual([true])

    Object.defineProperty(document, 'hidden', { configurable: true, get: () => true })
    document.dispatchEvent(new Event('visibilitychange'))
    await vi.advanceTimersByTimeAsync(0)
    Object.defineProperty(document, 'hidden', { configurable: true, get: () => false })

    expect(leaseBodies(calls)).toEqual([true, false])
    expect(current).toBe('1h')
    unsub()
  })
})
