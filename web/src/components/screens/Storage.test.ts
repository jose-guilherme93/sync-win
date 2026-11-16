import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it, vi } from 'vitest'
import Storage from './Storage.svelte'
import type { Device } from '../../lib/types'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' }
  })
}

const GB = 1024 ** 3
const now = new Date().toISOString()

function device(hardware: Record<string, unknown>): Device {
  return {
    id: 'dev-1',
    hostname: 'box',
    user_id: 'owner',
    status: 'online',
    last_seen_at: now,
    last_sync_at: now,
    app_count: 0,
    preference_count: 0,
    saves_count: 0,
    saves_size_bytes: 0,
    hardware: {
      cpu_usage_percent: 5,
      memory_used_bytes: 1,
      memory_total_bytes: 2,
      disk_read_rate: 1024,
      disk_write_rate: 2048,
      uptime_seconds: 100,
      load_average: '0',
      power_watts: 0,
      collected_at: now,
      ...hardware
    } as never
  } as Device
}

function stubHistory(mounts: Record<string, unknown[]>) {
  vi.stubGlobal(
    'fetch',
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input)
      if (url.includes('/storage/history')) return json({ device_id: 'dev-1', days: 30, mounts })
      return json([])
    })
  )
}

const PARTITIONS = [
  {
    mount: '/',
    device: 'nvme0n1p2',
    fstype: 'ext4',
    options: 'rw,relatime',
    total_bytes: 100 * GB,
    used_bytes: 42 * GB,
    free_bytes: 58 * GB,
    avail_bytes: 58 * GB,
    reserved_bytes: 5 * GB,
    used_percent: 42,
    inodes_total: 1000,
    inodes_used: 40,
    inodes_used_percent: 4
  },
  {
    mount: '/mnt/media',
    device: 'sdb1',
    fstype: 'xfs',
    options: 'rw,noatime',
    total_bytes: 1000 * GB,
    used_bytes: 910 * GB,
    free_bytes: 90 * GB,
    used_percent: 91,
    inodes_total: 0,
    inodes_used: 0,
    inodes_used_percent: 0
  }
]

describe('Storage screen', () => {
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  it('lists every mounted filesystem with its share and inode usage', async () => {
    stubHistory({})
    render(Storage, { props: { device: device({ disk_partitions: PARTITIONS }) } })

    expect((await screen.findAllByText('/mnt/media')).length).toBeGreaterThan(0)
    expect(screen.getAllByText('/').length).toBeGreaterThan(0)
    expect(screen.getByText('91%')).toBeTruthy()
    expect(screen.getByText('4%')).toBeTruthy()
    expect(screen.getByText('ext4')).toBeTruthy()
    expect(screen.getByText('xfs')).toBeTruthy()
  })

  it('puts a nearly full filesystem in the attention list', async () => {
    stubHistory({})
    render(Storage, { props: { device: device({ disk_partitions: PARTITIONS }) } })

    expect(await screen.findByText(/\/mnt\/media · 91% used/)).toBeTruthy()
    expect(screen.getAllByText('critical').length).toBeGreaterThan(0)
  })

  it('computes growth and an ETA from the stored days', async () => {
    stubHistory({
      '/': [
        { day: '2026-11-01', total_bytes: 100 * GB, used_bytes: 40 * GB, free_bytes: 60 * GB, inodes_used: 40, inodes_total: 1000 },
        { day: '2026-11-02', total_bytes: 100 * GB, used_bytes: 50 * GB, free_bytes: 50 * GB, inodes_used: 40, inodes_total: 1000 }
      ]
    })
    render(Storage, { props: { device: device({ disk_partitions: PARTITIONS }) } })

    // 10 GB/day against 50 GB free.
    expect(await screen.findByText('5d')).toBeTruthy()
    expect(screen.getByText(/full in ~5d/)).toBeTruthy()
    expect(screen.getByText('+10.0 GB')).toBeTruthy()
  })

  it('says the history starts today when no daily row exists yet', async () => {
    stubHistory({})
    render(Storage, { props: { device: device({ disk_partitions: PARTITIONS }) } })

    expect(await screen.findByText(/No daily history yet/)).toBeTruthy()
    expect(screen.getByText('needs more than a day of history')).toBeTruthy()
  })

  it('shows disks and reports SMART as unavailable instead of healthy', async () => {
    stubHistory({})
    render(Storage, {
      props: {
        device: device({
          disk_partitions: PARTITIONS,
          disk_devices: [
            {
              name: 'sdb',
              model: 'IronWolf 4TB',
              size_bytes: 4000 * GB,
              rotational: true,
              temperature_c: 33,
              smart: { available: false, reason: 'smartctl not installed', passed: false }
            }
          ]
        })
      }
    })

    expect(await screen.findByText('IronWolf 4TB')).toBeTruthy()
    expect(screen.getByText('HDD')).toBeTruthy()
    expect(screen.getByText('unavailable')).toBeTruthy()
  })

  it('lists past SMART self-tests with their result', async () => {
    stubHistory({})
    render(Storage, {
      props: {
        device: device({
          disk_partitions: PARTITIONS,
          disk_devices: [
            {
              name: 'nvme0n1',
              model: '990 PRO',
              size_bytes: 1000 * GB,
              rotational: false,
              smart: {
                available: true,
                passed: true,
                tests: [
                  { type: 'Short', result: 'Completed without error', hours: 12600 },
                  { type: 'Extended', result: 'Completed without error', hours: 12580 }
                ]
              }
            }
          ]
        })
      }
    })

    expect(await screen.findByText('SMART self-tests · newest first')).toBeTruthy()
    expect(screen.getAllByText(/Completed without error/).length).toBe(2)
    expect(screen.getByText(/at 12600 h power-on/)).toBeTruthy()
  })

  it('does not call a drive failing when smartctl had no verdict', async () => {
    stubHistory({})
    render(Storage, {
      props: {
        device: device({
          disk_partitions: PARTITIONS,
          disk_devices: [
            {
              name: 'sda',
              model: 'QEMU HARDDISK',
              size_bytes: 100 * GB,
              rotational: true,
              smart: { available: true, passed: false, status_known: false }
            }
          ]
        })
      }
    })

    expect(await screen.findByText('no verdict')).toBeTruthy()
  })

  it('flags a read-only mount', async () => {
    stubHistory({})
    render(Storage, {
      props: {
        device: device({
          disk_partitions: [
            {
              mount: '/data',
              device: 'sdc1',
              fstype: 'ext4',
              options: 'ro,relatime',
              read_only: true,
              total_bytes: 10 * GB,
              used_bytes: 2 * GB,
              free_bytes: 8 * GB,
              used_percent: 20
            }
          ]
        })
      }
    })

    expect(await screen.findByText(/mounted read-only/)).toBeTruthy()
    expect(screen.getByText('ro')).toBeTruthy()
    expect(screen.getAllByText('/data').length).toBeGreaterThan(0)
  })
})
