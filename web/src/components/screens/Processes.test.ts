import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { flushSync, tick } from 'svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import Processes from './Processes.svelte'
import type { ProcessInfo } from '../../lib/types'

const MB = 1024 ** 2

function proc(overrides: Partial<ProcessInfo> & { pid: number; name: string }): ProcessInfo {
  return {
    cpu_percent: 0,
    mem_rss_bytes: 0,
    state: 'S',
    ...overrides
  }
}

function device(hardware: Record<string, unknown>) {
  return {
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
      cpu_usage_percent: 30,
      memory_used_bytes: 4 * 1024 ** 3,
      memory_total_bytes: 8 * 1024 ** 3,
      uptime_seconds: 5000,
      load_average: '0.8 0.5 0.4',
      cpu_core_usage: [10, 20, 30, 40],
      collected_at: '2026-10-21T12:00:00Z',
      ...hardware
    }
  } as any
}

const worker = proc({
  pid: 100,
  ppid: 1,
  name: 'nginx',
  user: 'root',
  state: 'R',
  cpu_percent: 25,
  mem_rss_bytes: 100 * MB,
  threads: 4,
  elapsed_seconds: 3600,
  command: 'nginx: worker process'
})
const database = proc({
  pid: 200,
  ppid: 1,
  name: 'postgres',
  user: 'postgres',
  state: 'S',
  cpu_percent: 2,
  mem_rss_bytes: 1024 * MB,
  threads: 12,
  elapsed_seconds: 7200,
  command: '/usr/lib/postgresql/bin/postgres -D /var/lib/postgresql'
})
const stopped = proc({
  pid: 300,
  ppid: 250,
  name: 'backup.sh',
  user: 'alice',
  state: 'T',
  elapsed_seconds: 600,
  command: '/bin/bash /home/alice/backup.sh'
})
const zombie = proc({
  pid: 400,
  ppid: 1,
  name: 'old-proc',
  user: 'bob',
  state: 'Z',
  elapsed_seconds: 120,
  command: '[old-proc]'
})

function telemetry(overrides: Record<string, unknown> = {}) {
  return {
    top_cpu_processes: [worker, database],
    top_mem_processes: [database, worker],
    stopped_processes: [stopped],
    zombie_processes: [zombie],
    process_summary: {
      total: 500,
      running: 3,
      sleeping: 480,
      disk_sleep: 1,
      stopped: 1,
      zombie: 1,
      other: 14,
      threads: 1200
    },
    ...overrides
  }
}

describe('Processes screen', () => {
  afterEach(cleanup)

  it('shows the whole-machine tally, not just the reported rows', () => {
    render(Processes, { props: { device: device(telemetry()) } })
    expect(document.body.textContent).toContain('4 reported of 500 processes')
  })

  it('falls back to the reported subset on an older agent payload', () => {
    const { process_summary, stopped_processes, zombie_processes, ...legacy } = telemetry()
    void process_summary
    void stopped_processes
    void zombie_processes
    render(Processes, { props: { device: device(legacy) } })
    expect(document.body.textContent).toContain('Counts are the reported subset')
  })
})

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

function procRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    pid: 100,
    ppid: 1,
    name: 'nginx',
    cmd: 'nginx: worker process',
    user: 'www-data',
    uid: 33,
    state: 'R',
    threads: 4,
    rss_bytes: 100 * 1024 * 1024,
    vsz_bytes: 0,
    swap_bytes: 0,
    cpu_percent: 25,
    cpu_time_seconds: 120,
    start_time_unix: 1_700_000_000,
    uptime_seconds: 3600,
    io_read_bps: 2048,
    io_write_bps: 1024,
    fd_count: 12,
    oom_score: 300,
    nice: 0,
    prio: 20,
    cgroup: '',
    unit: 'nginx.service',
    container: '',
    ports: [],
    kernel: false,
    ...overrides
  }
}

function snapshotPayload(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    processes: [
      procRow({ pid: 100, name: 'parentproc', cmd: 'parentcmd --flag', cpu_percent: 25, ppid: 0 }),
      procRow({ pid: 101, name: 'childproc', cmd: 'childcmd', cpu_percent: 5, ppid: 100 }),
      procRow({ pid: 200, name: 'postgres', cmd: '/usr/lib/postgresql/bin/postgres', cpu_percent: 2, ppid: 1, state: 'S', rss_bytes: 1024 * 1024 * 1024 }),
      procRow({ pid: 300, name: 'backup.sh', cmd: '/bin/bash backup.sh', cpu_percent: 0, state: 'T', ppid: 250 }),
      procRow({ pid: 400, name: 'old-proc', cmd: '[old-proc]', cpu_percent: 0, state: 'Z', ppid: 1 }),
      procRow({ pid: 1, name: 'systemd', cmd: '/sbin/init', cpu_percent: 0.1, ppid: 0, state: 'S', unit: 'init.scope' })
    ],
    stats: {
      total: 500,
      threads: 1200,
      running: 3,
      sleeping: 480,
      stopped: 1,
      zombie: 1,
      blocked: 1,
      idle: 0,
      load1: 0.8,
      load5: 0.5,
      load15: 0.4,
      psi_cpu_some_avg10: 0,
      psi_mem_some_avg10: 0,
      psi_io_some_avg10: 0,
      oom_kills_24h: 0
    },
    collected_at: '2026-10-21T12:00:00Z',
    io_available: true,
    signals_enabled: true,
    ...overrides
  }
}

function historyPayload(): Record<string, unknown> {
  return {
    series: [{ name: 'parentproc', points: [[1_700_000_000, 25, 100], [1_700_000_060, 10, 105]] }],
    events: [{ t: 1_700_000_060, type: 'started', pid: 100, name: 'parentproc' }],
    buckets: [{ t: 1_700_000_000, top: [{ name: 'parentproc', cpu: 25 }, { name: 'postgres', cpu: 2 }] }]
  }
}

function historyIOPayload(): Record<string, unknown> {
  return {
    series: [
      {
        name: 'parentproc',
        points: [
          [1_700_000_000, 25, 100, 4096, 1024],
          [1_700_000_060, 10, 105, 8192, 2048]
        ]
      }
    ],
    events: [{ t: 1_700_000_060, type: 'started', pid: 100, name: 'parentproc' }],
    buckets: [{ t: 1_700_000_000, top: [{ name: 'parentproc', cpu: 25 }] }]
  }
}

describe('Processes screen interactions', () => {
  let calls: string[] = []
  let respond: (url: string) => Response = () => json(snapshotPayload())

  beforeEach(() => {
    calls = []
    respond = (url) => (url.includes('/processes/history') ? json(historyPayload()) : json(snapshotPayload()))
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        calls.push(url)
        return respond(url)
      })
    )
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('filters rows by search (name/cmdline/pid) and shows empty state', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())

    // The query lives behind the corner icon, not in a toolbar row.
    await fireEvent.click(screen.getByLabelText('Search'))
    const box = screen.getByLabelText('Search processes')
    await fireEvent.input(box, { target: { value: 'postgres' } })
    await vi.waitFor(() => expect(screen.getByText('postgres')).toBeTruthy())
    expect(screen.queryByText('parentproc')).toBeNull()

    await fireEvent.input(box, { target: { value: 'childcmd' } })
    await vi.waitFor(() => expect(document.querySelector('tbody')?.textContent ?? '').toContain('childproc'))
    expect(screen.queryByText('postgres')).toBeNull()

    await fireEvent.input(box, { target: { value: '999999' } })
    await vi.waitFor(() => expect(screen.getByText('No processes match.')).toBeTruthy())
  })

  it('opens the search popover with Ctrl K, closes it with Escape and marks the icon', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())
    expect(screen.queryByLabelText('Search processes')).toBeNull()

    await fireEvent.keyDown(window, { key: 'k', ctrlKey: true })
    await vi.waitFor(() => expect(screen.getByLabelText('Search processes')).toBeTruthy())

    const box = screen.getByLabelText('Search processes') as HTMLInputElement
    await fireEvent.input(box, { target: { value: 'postgres' } })
    await vi.waitFor(() => expect(document.querySelector('.sbtn.on')).toBeTruthy())
    expect(document.querySelector('.smark')).toBeTruthy()

    // Enter keeps the query, so the list stays filtered.
    await fireEvent.keyDown(box, { key: 'Enter' })
    await vi.waitFor(() => expect(screen.queryByLabelText('Search processes')).toBeNull())
    expect(screen.queryByText('parentproc')).toBeNull()

    // The clear affordance lives in the popover and empties the query.
    await fireEvent.click(screen.getByLabelText('Search'))
    await fireEvent.click(screen.getByLabelText('Clear search'))
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())
    expect(document.querySelector('.smark')).toBeNull()

    await fireEvent.keyDown(screen.getByLabelText('Search processes'), { key: 'Escape' })
    await vi.waitFor(() => expect(screen.queryByLabelText('Search processes')).toBeNull())
  })

  it('renders the reported rows and the tally when the agent predates the full list', async () => {
    respond = (url) =>
      url.includes('/processes/history')
        ? json({ series: [], events: [], buckets: [] })
        : json({ processes: [], stats: null, collected: false, io_available: false, signals_enabled: false })
    render(Processes, { props: { deviceId: 'dev-1', device: device(telemetry()) } })

    await vi.waitFor(() => expect(document.querySelectorAll('tbody tr').length).toBeGreaterThan(0))
    const tiles = Array.from(document.querySelectorAll('.sitem'))
    expect(tiles.some((t) => t.textContent?.includes('Processes'))).toBe(true)
    expect(document.body.textContent).toContain('reported of 500')
  })

  it('state chip filters narrow the table and toggle off restores', async () => {    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())

    await fireEvent.click(screen.getByRole('button', { name: /^Z\d/ }))
    await vi.waitFor(() => expect(screen.getByText('old-proc')).toBeTruthy())
    expect(screen.queryByText('parentproc')).toBeNull()

    await fireEvent.click(screen.getByRole('button', { name: /^Z\d/ }))
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())
  })

  it('clicking a numeric header reverses order', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())

    const before = document.querySelector('tbody tr')?.textContent ?? ''
    const cpuHeader = Array.from(document.querySelectorAll('th')).find((th) => th.textContent?.startsWith('CPU')) as HTMLElement
    await fireEvent.click(cpuHeader.querySelector('button') ?? cpuHeader)
    await vi.waitFor(() => {
      const after = document.querySelector('tbody tr')?.textContent ?? ''
      expect(after).not.toBe(before)
    })
  })

  it('grouping by process tree nests children under their parent', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())

    await fireEvent.click(screen.getByText('parentproc').closest('tr') as HTMLElement)
    await vi.waitFor(() => expect(document.querySelector('aside.drawer')).toBeTruthy())

    await fireEvent.click(screen.getByRole('button', { name: 'Process tree' }))
    await vi.waitFor(() => {
      const cells = Array.from(document.querySelectorAll('td .namecell')) as HTMLElement[]
      const child = cells.find((cell) => cell.textContent?.includes('childproc'))
      expect(child?.getAttribute('style')).toContain('0.9rem')
    })
    expect(document.querySelector('aside.drawer')).toBeTruthy()

    await fireEvent.click(screen.getByRole('button', { name: 'Flat' }))
    await vi.waitFor(() => {
      const cells = Array.from(document.querySelectorAll('td .namecell')) as HTMLElement[]
      const child = cells.find((cell) => cell.textContent?.includes('childproc'))
      expect(child?.getAttribute('style')).toContain('0rem')
    })
  })

  it('collapsing a subtree in the process tree group hides its children', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())

    await fireEvent.click(screen.getByRole('button', { name: 'Process tree' }))
    await vi.waitFor(() => expect(screen.getByText('childproc')).toBeTruthy())

    let parentRow = screen.getByText('parentproc').closest('tr') as HTMLElement
    await fireEvent.click(parentRow.querySelector('button.exp') as HTMLElement)
    await vi.waitFor(() => expect(screen.queryByText('childproc')).toBeNull())

    parentRow = screen.getByText('parentproc').closest('tr') as HTMLElement
    await fireEvent.click(parentRow.querySelector('button.exp') as HTMLElement)
    await vi.waitFor(() => expect(screen.getByText('childproc')).toBeTruthy())
  })

  it('clicking a row opens the drawer with the process identity', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())

    await fireEvent.click(screen.getByText('parentproc').closest('tr') as HTMLElement)
    await vi.waitFor(() => expect(document.querySelector('.cmdblock code')).toBeTruthy())
    const code = document.querySelector('.cmdblock code') as HTMLElement
    expect(code.textContent).toBe('parentcmd --flag')
    expect(screen.getByText(/100 · ppid 0/)).toBeTruthy()
  })

  it('kill popover confirm stays disabled until the pid is typed', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('postgres')).toBeTruthy())

    const row = screen.getByText('postgres').closest('tr') as HTMLElement
    await fireEvent.click(row.querySelector('button[title="KILL"]') as HTMLElement)
    await vi.waitFor(() => expect(screen.getByRole('dialog', { name: 'Kill confirmation' })).toBeTruthy())
    expect(screen.getByText('Kill postgres')).toBeTruthy()

    const confirm = screen.getByRole('button', { name: 'Kill' }) as HTMLButtonElement
    expect(confirm.disabled).toBe(true)

    const input = screen.getByPlaceholderText('200') as HTMLInputElement
    await fireEvent.input(input, { target: { value: '200' } })
    await vi.waitFor(() => expect((screen.getByRole('button', { name: 'Kill' }) as HTMLButtonElement).disabled).toBe(false))

    await fireEvent.keyDown(window, { key: 'Escape' })
    await vi.waitFor(() => expect(screen.queryByRole('dialog', { name: 'Kill confirmation' })).toBeNull())
  })

  it('kill popover blocked variant shows Blocked · protected and no input', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('systemd')).toBeTruthy())

    const row = screen.getByText('systemd').closest('tr') as HTMLElement
    await fireEvent.click(row.querySelector('button[title="KILL"]') as HTMLElement)
    await vi.waitFor(() => expect(screen.getByText('Blocked · protected')).toBeTruthy())
    expect(screen.queryByPlaceholderText('1')).toBeNull()
    expect(screen.queryByRole('button', { name: 'Kill' })).toBeNull()
    expect(screen.getByRole('button', { name: 'Close' })).toBeTruthy()

    await fireEvent.keyDown(window, { key: 'Escape' })
    await vi.waitFor(() => expect(screen.queryByRole('dialog', { name: 'Kill confirmation' })).toBeNull())
  })

  it('history range switch and a heatmap cell filter the live view to that window', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())

    await fireEvent.click(screen.getByRole('button', { name: 'History' }))
    await vi.waitFor(() => expect(document.querySelectorAll('.hmc').length).toBeGreaterThan(0))

    await fireEvent.click(screen.getByRole('button', { name: '1h' }))
    await vi.waitFor(() => expect(calls.some((u) => u.includes('range=1h'))).toBe(true), { timeout: 2000 })

    await fireEvent.click(screen.getByRole('button', { name: '7d' }))
    await vi.waitFor(() => expect(calls.some((u) => u.includes('range=7d'))).toBe(true))

    const cell = document.querySelector('.hmc:not(.empty)') as HTMLElement
    expect(cell).toBeTruthy()
    await fireEvent.click(cell)
    await vi.waitFor(() => expect(document.querySelector('.wchip')).toBeTruthy())
    // The chip carries the window range and how many rows it keeps.
    expect(document.querySelector('.wchip')?.textContent).toContain('2')
  })

  it('renders not collected when processes_collected is false', async () => {
    respond = (url) => (url.includes('/processes/history') ? json(historyPayload()) : json(snapshotPayload({ processes: [], stats: null, processes_collected: false })))
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText(/Not collected/)).toBeTruthy())
  })

  it('renders an em dash for IO when io_available is false', async () => {
    respond = (url) => (url.includes('/processes/history') ? json(historyPayload()) : json(snapshotPayload({ io_available: false })))
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())
    const io = document.querySelector('.ioval') as HTMLElement
    expect(io.textContent?.trim()).toBe('—')
  })
})

describe('Processes screen additions', () => {
  let calls: string[] = []
  let respond: (url: string) => Response = () => json(snapshotPayload())

  beforeEach(() => {
    calls = []
    respond = (url) => (url.includes('/processes/history') ? json(historyPayload()) : json(snapshotPayload()))
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        calls.push(url)
        return respond(url)
      })
    )
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('draws a sparkline from the loaded history', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())
    await vi.waitFor(() => expect(document.querySelector('.spark')).toBeTruthy())
  })

  it('drawer shows parent chain section', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())
    await fireEvent.click(screen.getByText('parentproc').closest('tr') as HTMLElement)
    await vi.waitFor(() => expect(document.querySelector('aside.drawer')).toBeTruthy())
    expect(document.body.textContent).toContain('Parent chain')
    expect(document.body.textContent).toContain('Direct children')
  })

  it('drawer shows listening sockets section', async () => {
    respond = (url) => (url.includes('/processes/history') ? json(historyPayload()) : json(snapshotPayload()))
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())
    await fireEvent.click(screen.getByText('postgres').closest('tr') as HTMLElement)
    await vi.waitFor(() => expect(document.querySelector('aside.drawer')).toBeTruthy())
    expect(document.body.textContent).toContain('Listening sockets')
  })

  it('history panels present with empty data', async () => {
    respond = (url) => (url.includes('/processes/history') ? json({ series: [], events: [], buckets: [] }) : json(snapshotPayload()))
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())
    await fireEvent.click(screen.getByRole('button', { name: 'History' }))
    await vi.waitFor(() => expect(document.body.textContent).toContain('Top CPU over time'))
    expect(document.body.textContent).toContain('Spike investigation')
    expect(document.body.textContent).toContain('Lifecycle events')
  })

  it('says IO is not recorded when the server sends three-value points', async () => {
    respond = (url) => (url.includes('/processes/history') ? json(historyPayload()) : json(snapshotPayload()))
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())
    await fireEvent.click(screen.getByRole('button', { name: 'History' }))
    await vi.waitFor(() => expect(document.body.textContent).toContain('IO is not recorded over time.'))
  })

  it('plots the IO panel when the server sends IO rates', async () => {
    respond = (url) => (url.includes('/processes/history') ? json(historyIOPayload()) : json(snapshotPayload()))
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())
    await fireEvent.click(screen.getByRole('button', { name: 'History' }))
    await vi.waitFor(() => expect(document.body.textContent).toContain('Spike investigation'))
    expect(document.body.textContent).not.toContain('IO is not recorded over time.')
  })

  it('KPI summary is one compact bar with a label and a value per item', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())

    const bar = document.querySelector('.kpi-bar') as HTMLElement
    expect(bar).toBeTruthy()
    // The compact class is what pins the bar under 56px; the old tile grid is gone.
    expect(bar.classList.contains('compact')).toBe(true)
    expect(document.querySelector('.kpis')).toBeNull()

    const labels = Array.from(bar.querySelectorAll('.sitem .k')).map((node) => node.textContent?.trim())
    expect(labels).toEqual(['Processes', 'Threads', 'Zombies', 'Running', 'Load', 'CPU pressure', 'OOM 24h'])
    // Every item is label + value on one line, and the sub-detail is a number.
    for (const item of Array.from(bar.querySelectorAll('.sitem'))) {
      expect(item.querySelector('.v')?.textContent?.trim()).toBeTruthy()
    }
    expect(bar.textContent).toContain('1200')
    expect(bar.textContent).toContain('1 blocked')
    expect(bar.textContent).toContain('0.50 · 0.40')
    expect(bar.textContent).toContain('6 shown')
  })

  it('tree pane lists threads, IO and uptime next to CPU and memory', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())

    await fireEvent.click(screen.getByRole('button', { name: 'Tree + inspector' }))
    await vi.waitFor(() => expect(document.querySelectorAll('button.pnode').length).toBeGreaterThan(1))

    const head = document.querySelector('.tline.thead') as HTMLElement
    expect(head.textContent).toContain('THR')
    expect(head.textContent).toContain('IO r/w')
    expect(head.textContent).toContain('UP')
    expect(head.textContent).toContain('CPU')
    expect(head.textContent).toContain('MEM')

    const node = Array.from(document.querySelectorAll('button.pnode')).find((n) =>
      n.textContent?.includes('parentproc')
    ) as HTMLElement
    expect(node.querySelector('.tthr')?.textContent?.trim()).toBe('4')
    expect(node.querySelector('.tio')?.textContent).toBe('2.0 KB/s / 1.0 KB/s')
    expect(node.querySelector('.tup')?.textContent?.trim()).toBe('1h')
    expect(node.querySelector('.tcpu')?.textContent?.trim()).toBe('25.0%')
    expect(node.querySelector('.tmem')?.textContent?.trim()).toMatch(/^100(\.0)? MB$/)
  })

  it('derives the user and unit selects from the loaded rows', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())

    const users = screen.getByLabelText('All users') as HTMLSelectElement
    expect(Array.from(users.options).map((option) => option.value)).toEqual(['', 'www-data'])
    const units = screen.getByLabelText('All units') as HTMLSelectElement
    expect(Array.from(units.options).map((option) => option.value)).toEqual([
      '',
      'init.scope',
      'nginx.service'
    ])
    expect(screen.queryByLabelText('All containers')).toBeNull()
  })

  it('tree + inspector shows the persistent detail for the selected process', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())

    await fireEvent.click(screen.getByRole('button', { name: 'Tree + inspector' }))
    await vi.waitFor(() => expect(document.querySelectorAll('.pnode').length).toBeGreaterThan(1))
    expect(document.querySelector('aside.drawer')).toBeNull()

    // The busiest row is selected on arrival, so the pane is never empty.
    const inspector = document.querySelector('.inspector') as HTMLElement
    expect(inspector.textContent).toContain('parentproc')
    expect(inspector.textContent).toContain('pid 100')
    for (const label of ['User', 'State', 'Threads', 'FDs', 'OOM score', 'Nice', 'Started', 'CPU time', 'RSS / swap', 'Unit', 'Cgroup', 'Container']) {
      expect(inspector.textContent).toContain(label)
    }
    expect(inspector.textContent).toContain('Parent chain')
    expect(inspector.textContent).toContain('Direct children')
    expect(inspector.textContent).toContain('Timeline')
    expect(inspector.textContent).toContain('CPU %')
    expect(inspector.textContent).toContain('RSS MB')
    expect(inspector.textContent).toContain('IO read / write KB/s')
  })

  it('draws the heatmap as the fixed 288-column bar grid', async () => {
    render(Processes, { props: { deviceId: 'dev-1' } })
    await vi.waitFor(() => expect(screen.getByText('parentproc')).toBeTruthy())

    await fireEvent.click(screen.getByRole('button', { name: 'History' }))
    await vi.waitFor(() => expect(document.querySelectorAll('.hmc').length).toBeGreaterThan(0))

    // History carries one bucket with two names: two rows of 288 bars.
    expect(document.querySelectorAll('.hmc').length).toBe(2 * 288)
    expect(document.querySelectorAll('.hmc.empty').length).toBe(2 * 288 - 2)
    const summaries = Array.from(document.querySelectorAll('.hmsum')).map((node) => node.textContent ?? '')
    expect(summaries[0]).toMatch(/Σ [\d.]+ core-min · peak [\d.]+%/)
    expect(document.body.textContent).toContain('288 × 5-min buckets')
    expect(document.body.textContent).toContain('process not alive')
    expect(document.querySelectorAll('.hmtick').length).toBeGreaterThanOrEqual(6)
    // name + pid, with the pid in the mono/faint style the prototype uses.
    const labels = Array.from(document.querySelectorAll('.hmlabel')).map((node) => node.textContent?.trim())
    expect(labels[0]).toContain('parentproc')
    expect(labels[0]).toContain('100')
  })
})
