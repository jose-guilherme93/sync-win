import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import DeviceModal from './DeviceModal.svelte'

// Chart.js needs a real canvas, which jsdom does not provide. The dashboard
// only builds charts when history exists, and these tests never return history,
// so a no-op mock is enough to let SystemMetrics import cleanly.
vi.mock('chart.js', () => {
  class Chart {
    static register() {}
    destroy() {}
    update() {}
  }
  return {
    Chart,
    LineController: {},
    LineElement: {},
    PointElement: {},
    LinearScale: {},
    CategoryScale: {},
    Filler: {},
    Tooltip: {},
    Legend: {}
  }
})

function json(body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status: 200,
    headers: { 'Content-Type': 'application/json' }
  })
}

const device = {
  id: 'dev-1',
  hostname: 'test-pc',
  user_id: 'alice',
  status: 'online',
  last_seen_at: new Date().toISOString(),
  last_sync_at: new Date().toISOString(),
  app_count: 0,
  preference_count: 0,
  saves_count: 0,
  saves_size_bytes: 0
}

const initialFiles = [
  {
    id: 'file-1',
    device_id: 'dev-1',
    user_id: 'alice',
    category: 'shell',
    filename: '.bashrc',
    relative_path: '.bashrc',
    content: 'x',
    content_hash: 'h',
    size_bytes: 1,
    synced_at: new Date().toISOString(),
    status: 'synced'
  }
]

describe('DeviceModal logs tab', () => {
  let detailCalls = 0

  beforeEach(() => {
    detailCalls = 0
    vi.stubGlobal(
      'fetch',
      vi.fn(async (input: RequestInfo | URL) => {
        const url = String(input)
        if (url.includes('/detail')) {
          detailCalls += 1
          // Empty logs is exactly the case that used to retrigger the fetch
          // forever through a self-referencing reactive statement.
          return json({ id: 'dev-1', hardware: { logs: [] } })
        }
        return json([])
      })
    )
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('loads device logs exactly once when the logs tab is opened', async () => {
    render(DeviceModal, { props: { device, open: true, initialFiles } })

    const filesTab = screen.getByRole('tab', { name: /files/i })

    // There is no Logs button; the panel is reached through the tablist
    // keyboard order: system -> files -> apps -> saves -> logs.
    for (let i = 0; i < 4; i++) {
      await fireEvent.keyDown(filesTab, { key: 'ArrowRight' })
    }

    await vi.waitFor(() => expect(detailCalls).toBeGreaterThanOrEqual(1))
    // Give any runaway reactive refetch a chance to fire; the fix must keep it
    // at exactly one request.
    await new Promise((resolve) => setTimeout(resolve, 80))
    expect(detailCalls).toBe(1)
  })
})
