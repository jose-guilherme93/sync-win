import { afterEach, describe, expect, it, vi } from 'vitest'
import { signInWithDevToken } from './dev-auth'

describe('development sign-in', () => {
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('does nothing without a token', async () => {
    const fetchMock = vi.fn()
    vi.stubGlobal('fetch', fetchMock)

    await expect(signInWithDevToken('')).resolves.toBe(false)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  it('exchanges the token for a session cookie', async () => {
    const fetchMock = vi.fn(async (_input: RequestInfo | URL, _init: RequestInit = {}) => new Response('{}', { status: 200 }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(signInWithDevToken('dev-token')).resolves.toBe(true)
    const [url, init] = fetchMock.mock.calls[0]
    if (!init) throw new Error('request init missing')
    expect(String(url)).toBe('/api/auth/dev')
    expect(init.method).toBe('POST')
    expect(JSON.parse(String(init.body))).toEqual({ token: 'dev-token' })
    expect(init.credentials).toBe('include')
  })

  it('falls back to the sign-in screen when the server refuses the token', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('{}', { status: 404 })))

    await expect(signInWithDevToken('dev-token')).resolves.toBe(false)
  })

  it('does not throw when the server is unreachable', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new Error('offline') }))

    await expect(signInWithDevToken('dev-token')).resolves.toBe(false)
  })
})
