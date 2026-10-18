import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  enqueueDockerRequest,
  fetchDockerState,
  openDockerStream,
  runDockerCommand,
  waitForDockerResult
} from './docker-client'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

function pending(): Response {
  return new Response(null, { status: 202 })
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('fetchDockerState', () => {
  it('normalises the cached snapshot', async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      json({ available: true, containers: [{ id: 'a', names: ['/web'], image: 'nginx' }], info: { version: '27.1' } })
    )
    vi.stubGlobal('fetch', fetchMock)

    const state = await fetchDockerState('dev-1')

    expect(fetchMock.mock.calls[0][0]).toBe('/api/devices/dev-1/docker')
    expect(state.containers[0].name).toBe('web')
    expect(state.info?.version).toBe('27.1')
  })

  it('carries the server reason into the error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(json({ error: 'device not found' }, 404)))
    await expect(fetchDockerState('gone')).rejects.toThrow('device not found')
  })
})

describe('enqueueDockerRequest', () => {
  it('posts the canonical request shape and returns the id', async () => {
    const fetchMock = vi.fn().mockResolvedValue(json({ id: 'dreq-1' }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(enqueueDockerRequest('dev-1', 'start', 'abc', '')).resolves.toBe('dreq-1')

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/api/devices/dev-1/docker')
    expect(init.method).toBe('POST')
    expect(JSON.parse(init.body)).toEqual({ type: 'start', target: 'abc', payload: '' })
  })

  it('reports a disabled mutation from the server', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn().mockResolvedValue(json({ error: 'Docker mutations are disabled' }, 503))
    )
    await expect(enqueueDockerRequest('dev-1', 'prune_system')).rejects.toThrow(
      'Docker mutations are disabled'
    )
  })
})

describe('waitForDockerResult', () => {
  it('keeps polling while the server answers 202, then returns the result', async () => {
    vi.useFakeTimers()
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(pending())
      .mockResolvedValueOnce(json({ status: 'completed', message: '[]' }))
    vi.stubGlobal('fetch', fetchMock)

    const wait = waitForDockerResult('dev-1', 'dreq-1')
    await vi.advanceTimersByTimeAsync(400)

    await expect(wait).resolves.toMatchObject({ status: 'completed', message: '[]' })
    expect(fetchMock).toHaveBeenCalledTimes(2)
    expect(fetchMock.mock.calls[1][0]).toBe('/api/devices/dev-1/docker/result/dreq-1')
  })

  it('gives up with a reason instead of waiting forever on an offline device', async () => {
    vi.useFakeTimers()
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(pending()))

    const wait = waitForDockerResult('dev-1', 'dreq-1', { timeoutMs: 1_000 })
    wait.catch(() => {})
    await vi.advanceTimersByTimeAsync(2_000)

    await expect(wait).rejects.toThrow(/did not answer within 1s/)
  })
})

describe('runDockerCommand', () => {
  it('rejects a failed command with the device reason', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json({ id: 'dreq-2' }))
      .mockResolvedValueOnce(json({ status: 'failed', message: 'command disabled by local policy' }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(runDockerCommand('dev-1', 'stop', 'abc')).rejects.toThrow(/local policy/)
  })

  it('resolves a completed command', async () => {
    const fetchMock = vi
      .fn()
      .mockResolvedValueOnce(json({ id: 'dreq-3' }))
      .mockResolvedValueOnce(json({ status: 'completed', message: 'ok' }))
    vi.stubGlobal('fetch', fetchMock)

    await expect(runDockerCommand('dev-1', 'restart', 'abc')).resolves.toMatchObject({ status: 'completed' })
  })
})

describe('openDockerStream', () => {
  it('reports unavailability when EventSource does not exist', () => {
    vi.stubGlobal('EventSource', undefined)
    const onError = vi.fn()

    const close = openDockerStream('dev-1', { onState: vi.fn(), onError })

    expect(onError).toHaveBeenCalledTimes(1)
    expect(() => close()).not.toThrow()
  })
})
