import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import SecurityTab from './SecurityTab.svelte'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

const audit = {
  id: 'audit-1',
  report_json: JSON.stringify({
    hardening_index: 72,
    total_warnings: 1,
    total_suggestions: 2,
    total_tests: 10,
    tests_passed: 8,
    warnings: [],
    suggestions: [],
    categories: []
  })
}

function stubAPI(handler: (url: string, method: string) => Response) {
  const calls: string[] = []
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    calls.push(`${init?.method || 'GET'} ${url}`)
    return handler(url, init?.method || 'GET')
  }))
  return calls
}

describe('SecurityTab Lynis flow', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('checks command status and loads the persisted report after completion', async () => {
    const calls = stubAPI((url, method) => {
      if (url.includes('/security/audit') && method === 'POST') return json({ id: 'cmd-1' })
      if (url.includes('/commands/cmd-1')) return json({ id: 'cmd-1', status: 'completed' })
      if (url.includes('/security/audits')) return json([audit])
      return json({ error: 'unexpected request' }, 404)
    })

    render(SecurityTab, { props: { deviceId: 'dev-1', lynisAvailable: true, agentStatus: 'online' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Run Audit' }))

    expect(await screen.findByText('Audit completed and report saved.')).toBeTruthy()
    expect(screen.getByText('Moderately Hardened')).toBeTruthy()
    expect(calls).toContain('GET /api/devices/dev-1/commands/cmd-1')
  })

  it('shows the agent-reported command failure instead of timing out', async () => {
    stubAPI((url, method) => {
      if (url.includes('/security/audit') && method === 'POST') return json({ id: 'cmd-2' })
      if (url.includes('/commands/cmd-2')) return json({ status: 'failed', message: 'Lynis is not installed' })
      if (url.includes('/security/audits')) return json([])
      return json({ error: 'unexpected request' }, 404)
    })

    render(SecurityTab, { props: { deviceId: 'dev-1', lynisAvailable: true, agentStatus: 'online' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Run Audit' }))

    expect(await screen.findByText('Lynis is not installed')).toBeTruthy()
    expect(screen.queryByText(/timed out/i)).toBeNull()
  })

  it('explains that a queued command will wait for an offline agent', async () => {
    stubAPI((url, method) => {
      if (url.includes('/security/audit') && method === 'POST') return json({ id: 'cmd-3' })
      if (url.includes('/commands/cmd-3')) return json({ status: 'queued' })
      if (url.includes('/security/audits')) return json([])
      return json({ error: 'unexpected request' }, 404)
    })

    render(SecurityTab, { props: { deviceId: 'dev-1', lynisAvailable: true, agentStatus: 'offline' } })
    await fireEvent.click(screen.getByRole('button', { name: 'Run Audit' }))

    expect(await screen.findByText(/device is offline.*run when the agent reconnects/i)).toBeTruthy()
  })

  it('shows the install guidance and does not queue an audit when Lynis is absent', async () => {
    const calls = stubAPI((url) => url.includes('/security/audits') ? json([]) : json({}))
    render(SecurityTab, { props: { deviceId: 'dev-1', lynisAvailable: false, lynisInstallCmd: 'sudo apt install lynis' } })

    expect(screen.getByText('Lynis not installed')).toBeTruthy()
    expect(screen.getByText('sudo apt install lynis')).toBeTruthy()
    expect(screen.queryByRole('button', { name: 'Run Audit' })).toBeNull()
    await vi.waitFor(() => expect(calls.every(call => !call.includes('POST'))).toBe(true))
  })
})
