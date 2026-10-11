import { cleanup, fireEvent, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Packages from './Packages.svelte'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

function stubAPI(handler: (url: string) => Response) {
  vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => handler(String(input))))
}

describe('Packages screen — pending updates', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('renders pending updates with current and new versions', async () => {
    stubAPI((url) => {
      if (url.includes('/updates')) {
        return json({
          status: 'ready',
          checked_at: '2026-10-07T12:00:00Z',
          updates: [
            { source: 'apt', name: 'vim', current_version: '9.1', new_version: '9.2' },
            { source: 'flatpak', name: 'org.example.App', new_version: '2.0' }
          ]
        })
      }
      return json([])
    })
    render(Packages, { props: { deviceId: 'dev-1' } })

    expect(await screen.findByText('vim')).toBeTruthy()
    expect(screen.getByText('9.1 → 9.2')).toBeTruthy()
    expect(screen.getByText('org.example.App')).toBeTruthy()
  })

  it('distinguishes an up-to-date device from a never-checked one', async () => {
    stubAPI((url) => url.includes('/updates')
      ? json({ status: 'ready', checked_at: '2026-10-07T12:00:00Z', updates: [] })
      : json([]))
    render(Packages, { props: { deviceId: 'dev-1' } })
    expect(await screen.findByText('Up to date')).toBeTruthy()
    expect(screen.queryByText('Not checked yet')).toBeNull()
  })

  it('shows an explicit unsupported state when no package manager exists', async () => {
    stubAPI((url) => url.includes('/updates')
      ? json({ status: 'unsupported', message: 'No supported package manager is installed on this device.', updates: [] })
      : json([]))
    render(Packages, { props: { deviceId: 'dev-1' } })
    expect(await screen.findByText('No supported package manager')).toBeTruthy()
  })

  it('shows a failed update check instead of an empty list', async () => {
    stubAPI((url) => url.includes('/updates')
      ? json({ status: 'error', message: 'pacman query failed', updates: [] })
      : json([]))
    render(Packages, { props: { deviceId: 'dev-1' } })
    expect((await screen.findByRole('alert')).textContent).toContain('pacman query failed')
  })

  it('surfaces a request failure with a retry that re-fetches', async () => {
    let attempts = 0
    stubAPI((url) => {
      if (url.includes('/updates')) {
        attempts += 1
        return attempts === 1 ? json({ error: 'boom' }, 500) : json({ status: 'ready', updates: [] })
      }
      return json([])
    })
    render(Packages, { props: { deviceId: 'dev-1' } })

    expect(await screen.findByText(/request failed \(500\)/)).toBeTruthy()
    await fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
    expect(await screen.findByText('Up to date')).toBeTruthy()
  })
})

describe('Packages screen — installed list', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  // What a Debian machine actually reported before the collector joined versions
  // in from dpkg: a name and nothing else.
  const APPS = [
    { source: 'pacman', name: 'vim', version: '9.1.1-1' },
    { source: 'apt', name: 'curl' },
    { source: 'pacman', name: 'zsh', version: '5.9-4' }
  ]

  function stubApps() {
    stubAPI((url) => (url.includes('/apps') ? json(APPS) : json({ status: 'ready', updates: [] })))
  }

  it('shows the reported version next to the package', async () => {
    stubApps()
    render(Packages, { props: { deviceId: 'dev-1' } })

    expect(await screen.findByText('vim')).toBeTruthy()
    expect(screen.getByText('9.1.1-1')).toBeTruthy()
    expect(screen.getByText('5.9-4')).toBeTruthy()
  })

  it('holds the version column when a package has none', async () => {
    stubApps()
    render(Packages, { props: { deviceId: 'dev-1' } })

    // An absent version must read as absent, not as an empty cell that looks
    // like the column is broken.
    const row = (await screen.findByText('curl')).closest('li') as HTMLElement
    expect(row.textContent).toContain('—')
  })

  it('says how many packages have no version instead of leaving it unexplained', async () => {
    stubApps()
    render(Packages, { props: { deviceId: 'dev-1' } })
    expect(await screen.findByText(/1 without a version/)).toBeTruthy()
  })

  it('counts the installed packages in the headline', async () => {
    stubApps()
    render(Packages, { props: { deviceId: 'dev-1' } })

    await screen.findByText('vim')
    const installed = screen.getByText('Installed').closest('.stat') as HTMLElement
    expect(installed.textContent).toMatch(/Installed\s*3/)
    expect(installed.textContent).toMatch(/2 sources/)
  })
})
