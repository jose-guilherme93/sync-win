import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

// api.ts reads window.location.origin and import.meta.env at module load, so
// each case resets the module registry and stubs the environment before import.

function csrfHeader(call: unknown[]): string | null {
  const init = call[1] as RequestInit
  return (init.headers as Headers).get('X-LEM-CSRF')
}

describe('api URL resolution', () => {
  beforeEach(() => vi.resetModules())
  afterEach(() => {
    vi.unstubAllEnvs()
    vi.unstubAllGlobals()
  })

  it('apiURL always returns a same-origin relative path', async () => {
    vi.stubEnv('VITE_API_BASE', '')
    const { apiURL } = await import('./api')
    expect(apiURL('/api/devices')).toBe('/api/devices')
    expect(apiURL('api/devices')).toBe('/api/devices')
  })

  it('serverBase falls back to the current origin when unset', async () => {
    vi.stubEnv('VITE_API_BASE', '')
    const { serverBase } = await import('./api')
    expect(serverBase).toBe(window.location.origin)
  })

  it('serverBase honors VITE_API_BASE and strips trailing slashes', async () => {
    vi.stubEnv('VITE_API_BASE', 'https://lem.example.com/')
    const { serverBase } = await import('./api')
    expect(serverBase).toBe('https://lem.example.com')
  })

  it('streamURL is same-origin and encodes the ticket', async () => {
    vi.stubEnv('VITE_API_BASE', '')
    const { streamURL } = await import('./api')
    expect(streamURL('/api/notifications/stream', 'a b/c')).toBe('/api/notifications/stream?ticket=a%20b%2Fc')
  })
})

describe('apiFetch CSRF handling', () => {
  beforeEach(() => {
    vi.resetModules()
    document.cookie = 'lem_csrf=; expires=Thu, 01 Jan 1970 00:00:00 GMT'
  })
  afterEach(() => {
    vi.unstubAllEnvs()
    vi.unstubAllGlobals()
    document.cookie = 'lem_csrf=; expires=Thu, 01 Jan 1970 00:00:00 GMT'
  })

  it('adds the CSRF header (url-decoded) and credentials for mutations', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    document.cookie = 'lem_csrf=tok%2F123'
    const { apiFetch } = await import('./api')

    await apiFetch('/api/x', { method: 'POST' })

    expect(fetchMock).toHaveBeenCalledTimes(1)
    expect(csrfHeader(fetchMock.mock.calls[0])).toBe('tok/123')
    expect((fetchMock.mock.calls[0][1] as RequestInit).credentials).toBe('include')
  })

  it('does not add the CSRF header for GET', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    document.cookie = 'lem_csrf=tok123'
    const { apiFetch } = await import('./api')

    await apiFetch('/api/x')

    expect(csrfHeader(fetchMock.mock.calls[0])).toBeNull()
  })

  it('does not crash on a malformed CSRF cookie', async () => {
    const fetchMock = vi.fn().mockResolvedValue(new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)
    document.cookie = 'lem_csrf=%E0%A4%A'
    const { apiFetch } = await import('./api')

    await expect(apiFetch('/api/x', { method: 'POST' })).resolves.toBeDefined()
    expect(csrfHeader(fetchMock.mock.calls[0])).toBeNull()
  })
})
