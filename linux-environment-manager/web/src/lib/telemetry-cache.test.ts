import { beforeEach, describe, expect, it, vi } from 'vitest'

// fake-indexeddb (see test-setup.ts) backs IndexedDB in jsdom. The cache keeps a
// module-level dbPromise, so each test re-imports and clears first.
describe('telemetry cache', () => {
  beforeEach(async () => {
    vi.resetModules()
    const { clearCache } = await import('./telemetry-cache')
    await clearCache()
  })

  it('round-trips a cached range', async () => {
    const { setCachedHistory, getCachedHistory } = await import('./telemetry-cache')
    const points = [{ cpu: 1 }, { cpu: 2 }]

    await setCachedHistory('dev-1', 'auto', 'from-1', 'to-1', points)

    expect(await getCachedHistory('dev-1', 'auto', 'from-1', 'to-1')).toEqual(points)
  })

  it('treats an entry older than maxAge as a miss', async () => {
    const { setCachedHistory, getCachedHistory } = await import('./telemetry-cache')
    await setCachedHistory('dev-2', 'auto', 'from', 'to', [{ cpu: 3 }])

    expect(await getCachedHistory('dev-2', 'auto', 'from', 'to', 0)).toBeNull()
  })

  it('misses when the time window differs', async () => {
    const { setCachedHistory, getCachedHistory } = await import('./telemetry-cache')
    await setCachedHistory('dev-3', 'auto', 'from-a', 'to-a', [{ cpu: 4 }])

    expect(await getCachedHistory('dev-3', 'auto', 'from-b', 'to-b')).toBeNull()
  })

  it('recovers after a transient IndexedDB open failure', async () => {
    const real = globalThis.indexedDB
    const failing = {
      open: () => {
        const req: { onerror?: () => void; onsuccess?: () => void; onupgradeneeded?: () => void } = {}
        queueMicrotask(() => req.onerror?.())
        return req
      }
    }
    vi.stubGlobal('indexedDB', failing)
    vi.resetModules()
    const mod = await import('./telemetry-cache')
    expect(await mod.getCachedHistory('d', 'auto', 'f', 't')).toBeNull()

    // The failed promise must not be cached: a later call succeeds.
    vi.stubGlobal('indexedDB', real)
    await mod.setCachedHistory('d', 'auto', 'f', 't', [{ x: 1 }])
    expect(await mod.getCachedHistory('d', 'auto', 'f', 't')).toEqual([{ x: 1 }])
  })
})
