import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import App from './App.svelte'

// EventSource is not implemented in jsdom; the dashboard opens one for the
// notification stream.
class MockEventSource {
  onmessage: ((ev: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null
  constructor(public url: string) {}
  close() {}
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

describe('App enrollment token', () => {
  let enrollCalls = 0

  beforeEach(() => {
    localStorage.clear()
    enrollCalls = 0
    vi.stubGlobal('EventSource', MockEventSource)
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/api/auth/me')) return json({}, 401)
        if (url.includes('/api/auth/login')) return json({ owner_id: 'owner-1', email: 'a@b.com' })
        if (url.includes('/api/agent/enroll-token')) {
          enrollCalls += 1
          // A persistent failure is what used to refire the request forever.
          return json({ error: 'boom' }, 500)
        }
        if (url.includes('/api/notifications/stream-token')) return json({ ticket: 't' })
        return json([])
      })
    )
    // The install panel is restored open from storage.
    localStorage.setItem('lem-install-open', '1')
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    localStorage.clear()
  })

  it('requests an enrollment token exactly once when sign-in fails to provide one', async () => {
    render(App)

    const email = await screen.findByPlaceholderText('you@example.com')
    const password = screen.getByPlaceholderText('At least 8 characters')
    await fireEvent.input(email, { target: { value: 'a@b.com' } })
    await fireEvent.input(password, { target: { value: 'password123' } })

    const form = screen.getByRole('button', { name: 'Sign in' }).closest('form')
    if (!form) throw new Error('sign-in form not found')
    await fireEvent.submit(form)

    await vi.waitFor(() => expect(enrollCalls).toBeGreaterThanOrEqual(1))
    await new Promise((resolve) => setTimeout(resolve, 100))
    expect(enrollCalls).toBe(1)
  })
})
