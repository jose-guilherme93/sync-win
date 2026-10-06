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
  handler: (url: string) => Response | Promise<Response>
): { calls: string[] } {
  const calls: string[] = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      calls.push(url)
      return handler(url)
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

    await vi.waitFor(() => expect(screen.getByText('1 failed')).toBeTruthy())
    expect(screen.getByText('1 running')).toBeTruthy()
    expect(screen.getByText('3 total')).toBeTruthy()
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
    expect(screen.getByText('No listening sockets reported')).toBeTruthy()
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
    await fireEvent.click(screen.getByRole('checkbox', { name: /problems only/i }))

    await vi.waitFor(() => expect(screen.queryByText('sshd.service')).toBeNull())
    expect(screen.getByText('nginx.service')).toBeTruthy()
  })

  it('does not request anything without a device id', async () => {
    const { calls } = stubFetch((url) => route(url, SERVICES, PORTS))
    render(Services, { props: { deviceId: '' } })

    await new Promise((resolve) => setTimeout(resolve, 40))
    expect(calls.length).toBe(0)
  })
})