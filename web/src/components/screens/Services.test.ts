import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Services from './Services.svelte'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

// The agent maps systemd onto these buckets and sorts failed units first.
const SERVICES = [
  {
    name: 'nginx.service',
    status: 'failed',
    load_state: 'loaded',
    active_state: 'failed',
    sub_state: 'failed',
    unit_file_state: 'enabled-runtime',
    description: 'A high performance web server',
    enabled: true
  },
  {
    name: 'sshd.service',
    status: 'running',
    load_state: 'loaded',
    active_state: 'active',
    sub_state: 'running',
    unit_file_state: 'enabled',
    description: 'OpenBSD Secure Shell server',
    enabled: true
  },
  {
    name: 'bluetooth.service',
    status: 'stopped',
    active_state: 'inactive',
    unit_file_state: 'disabled',
    enabled: false
  }
]

const PORTS = [
  { protocol: 'tcp', local_address: '', port: 22, process: 'sshd', pid: 812 },
  { protocol: 'tcp', local_address: '127.0.0.1', port: 8080, process: 'node', pid: 90 },
  { protocol: 'udp', local_address: '127.0.0.53', port: 53, process: 'systemd-resolve', pid: 750 }
]

function stubFetch(
  handler: (url: string, init?: RequestInit) => Response | Promise<Response>
): { calls: string[] } {
  const calls: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input)
      calls.push(url)
      return handler(url, init)
    })
  )
  return { calls }
}

function route(url: string, services: unknown, ports: unknown, status = 200): Response {
  if (url.includes('/services')) return json(services, status)
  if (url.includes('/ports')) return json(ports, status)
  return json([], status)
}

describe('Services screen', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    vi.useRealTimers()
  })

  it('requests both endpoints for the selected device', async () => {
    const { calls } = stubFetch((url) => route(url, SERVICES, PORTS))
    render(Services, { props: { deviceId: 'dev-1' } })

    await vi.waitFor(() => expect(screen.getByText('nginx.service')).toBeTruthy())
    expect(calls.some((u) => u.includes('/api/devices/dev-1/services'))).toBe(true)
    expect(calls.some((u) => u.includes('/api/devices/dev-1/ports'))).toBe(true)
  })

  it('renders the status bucket, not the raw systemd column', async () => {
    stubFetch((url) => route(url, SERVICES, PORTS))
    render(Services, { props: { deviceId: 'dev-1' } })

    await vi.waitFor(() => expect(screen.getByText('sshd.service')).toBeTruthy())
    // sshd reports active_state "active" with sub_state "running"; the row must
    // show the mapped bucket, not the raw ACTIVE value.
    const row = screen.getByText('sshd.service').closest('li') as HTMLElement
    expect(row.textContent).toContain('running')
    expect(row.textContent).not.toContain('enabled-runtime')
    // enabled-runtime is a real unit file state and must still be visible.
    expect(screen.getByText('enabled-runtime')).toBeTruthy()
  })

  it('summarises failed and running counts', async () => {
    stubFetch((url) => route(url, SERVICES, PORTS))
    render(Services, { props: { deviceId: 'dev-1' } })

    await vi.waitFor(() => expect(screen.getByText('nginx.service')).toBeTruthy())
    const failedTile = screen.getByText('Failed').closest('.tile') as HTMLElement
    const runningTile = screen.getByText('Running').closest('.tile') as HTMLElement
    const stoppedTile = screen.getByText('Stopped').closest('.tile') as HTMLElement
    expect(failedTile.textContent).toMatch(/Failed\s*1/)
    expect(runningTile.textContent).toMatch(/Running\s*1/)
    expect(stoppedTile.textContent).toMatch(/Stopped\s*1/)
  })

  it('shows a wildcard listener rather than an empty host', async () => {
    stubFetch((url) => route(url, SERVICES, PORTS))
    render(Services, { props: { deviceId: 'dev-1' } })

    await vi.waitFor(() => expect(screen.getByText('sshd')).toBeTruthy())
    expect(screen.getByText('*')).toBeTruthy()
    expect(screen.getByText('8080')).toBeTruthy()
  })

  it('separates a device that never reported from one that reported nothing', async () => {
    stubFetch((url) => route(url, [], []))
    render(Services, { props: { deviceId: 'dev-1' } })

    await vi.waitFor(() => expect(screen.getByText('No services reported')).toBeTruthy())
    expect(screen.getByText('No listening sockets')).toBeTruthy()
  })

  it('surfaces a failed request instead of claiming nothing was collected', async () => {
    stubFetch((url) => route(url, SERVICES, PORTS, 500))
    render(Services, { props: { deviceId: 'dev-1' } })

    // The message appears in both the banner and the card's empty state, so
    // assert on the alert rather than on the text alone.
    await vi.waitFor(() =>
      expect(screen.getByRole('alert').textContent).toMatch(/services request failed/i)
    )
    // An error must not be mistaken for "this device reported nothing".
    expect(screen.queryByText('No services reported')).toBeNull()
    expect(screen.queryByText('Not collected yet')).toBeNull()
  })

  it('retries after a failed request', async () => {
    let attempt = 0
    const { calls } = stubFetch((url) => {
      attempt += 1
      return attempt === 1 ? route(url, [], [], 500) : route(url, SERVICES, PORTS)
    })
    render(Services, { props: { deviceId: 'dev-1' } })

    await vi.waitFor(() => expect(screen.getByRole('button', { name: /retry/i })).toBeTruthy())
    await fireEvent.click(screen.getByRole('button', { name: /retry/i }))

    await vi.waitFor(() => expect(screen.getByText('nginx.service')).toBeTruthy())
    expect(calls.filter((u) => u.includes('/services')).length).toBe(2)
  })

  it('filters to problems only', async () => {
    stubFetch((url) => route(url, SERVICES, PORTS))
    render(Services, { props: { deviceId: 'dev-1' } })

    await vi.waitFor(() => expect(screen.getByText('sshd.service')).toBeTruthy())
    await fireEvent.click(screen.getByRole('checkbox', { name: /problems/i }))

    await vi.waitFor(() => expect(screen.queryByText('sshd.service')).toBeNull())
    expect(screen.getByText('nginx.service')).toBeTruthy()
  })

  it('renders instead of blanking when the agent reports one socket twice', async () => {
    // Real `ss` output: the collector folds 0.0.0.0 and * onto the empty host, so
    // mDNS on 5353 arrives twice. A keyed each throws each_key_duplicate on that,
    // which aborts the component's render — the screen stayed frozen on
    // "Loading system inventory…" and never recovered.
    const duplicated = [
      { protocol: 'udp', local_address: '', port: 5353 },
      { protocol: 'udp', local_address: '', port: 5353 },
      { protocol: 'udp', local_address: '::', port: 5353 },
      { protocol: 'udp', local_address: '::', port: 5353 },
      ...PORTS
    ]
    stubFetch((url) => route(url, SERVICES, duplicated))
    render(Services, { props: { deviceId: 'dev-1' } })

    await vi.waitFor(() => expect(screen.getByText('nginx.service')).toBeTruthy())
    expect(screen.queryByText('Loading system inventory…')).toBeNull()
    // Four reported rows collapse to the two real sockets: one IPv4 wildcard and
    // one IPv6 wildcard, each shown once.
    expect(screen.getAllByText('5353')).toHaveLength(2)
  })

  it('does not request anything without a device id', async () => {
    const { calls } = stubFetch((url) => route(url, SERVICES, PORTS))
    render(Services, { props: { deviceId: '' } })

    await new Promise((resolve) => setTimeout(resolve, 40))
    expect(calls.length).toBe(0)
  })

  it('keeps the card that loaded when only the other endpoint fails', async () => {
    // Promise.all would reject here and discard the ports answer entirely.
    stubFetch((url) => (url.includes('/ports') ? json(PORTS) : json([], 500)))
    render(Services, { props: { deviceId: 'dev-1' } })

    await vi.waitFor(() => expect(screen.getByText('8080')).toBeTruthy())
    // The failure names its own endpoint and keeps the status code.
    expect(screen.getByText('services request failed (500)')).toBeTruthy()
    // An unreadable endpoint must not read as a device that reported nothing.
    expect(screen.queryByText('No services reported')).toBeNull()
    // A half that loaded is not a total outage, so no blanket error banner.
    expect(screen.queryByRole('alert')).toBeNull()
  })

  it('gives up on a request that never settles instead of loading forever', async () => {
    vi.useFakeTimers()
    // A response that never resolves and only ends when aborted is the case that
    // used to freeze the screen on "Loading system inventory…" indefinitely.
    stubFetch((url, init) =>
      url.includes('/services')
        ? new Promise<Response>((_, reject) => {
            init?.signal?.addEventListener('abort', () => {
              // What a real fetch rejects with when the signal fires.
              const aborted = new Error('This operation was aborted')
              aborted.name = 'AbortError'
              reject(aborted)
            })
          })
        : json(PORTS)
    )
    render(Services, { props: { deviceId: 'dev-1' } })

    await vi.advanceTimersByTimeAsync(9000)

    // A deadline is reported as a deadline, not as a generic crash string.
    expect(screen.getByText('services timed out after 8s')).toBeTruthy()
    expect(screen.queryByText('Loading system inventory…')).toBeNull()
  })
})