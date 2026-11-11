import { cleanup, render, screen } from '@testing-library/svelte'
import { afterEach, describe, expect, it } from 'vitest'
import Processes from './Processes.svelte'
import type { ProcessInfo } from '../../lib/types'

const MB = 1024 ** 2

function proc(overrides: Partial<ProcessInfo> & { pid: number; name: string }): ProcessInfo {
  return {
    cpu_percent: 0,
    mem_rss_bytes: 0,
    state: 'S',
    ...overrides
  }
}

function device(hardware: Record<string, unknown>) {
  return {
    id: 'dev-1',
    hostname: 'workstation',
    user_id: 'user-1',
    status: 'online',
    last_seen_at: '2026-10-21T12:00:00Z',
    last_sync_at: '2026-10-21T12:00:00Z',
    app_count: 0,
    preference_count: 0,
    saves_count: 0,
    saves_size_bytes: 0,
    hardware: {
      cpu_usage_percent: 30,
      memory_used_bytes: 4 * 1024 ** 3,
      memory_total_bytes: 8 * 1024 ** 3,
      uptime_seconds: 5000,
      load_average: '0.8 0.5 0.4',
      cpu_core_usage: [10, 20, 30, 40],
      collected_at: '2026-10-21T12:00:00Z',
      ...hardware
    }
  } as any
}

const worker = proc({
  pid: 100,
  ppid: 1,
  name: 'nginx',
  user: 'root',
  state: 'R',
  cpu_percent: 25,
  mem_rss_bytes: 100 * MB,
  threads: 4,
  elapsed_seconds: 3600,
  command: 'nginx: worker process'
})
const database = proc({
  pid: 200,
  ppid: 1,
  name: 'postgres',
  user: 'postgres',
  state: 'S',
  cpu_percent: 2,
  mem_rss_bytes: 1024 * MB,
  threads: 12,
  elapsed_seconds: 7200,
  command: '/usr/lib/postgresql/bin/postgres -D /var/lib/postgresql'
})
const stopped = proc({
  pid: 300,
  ppid: 250,
  name: 'backup.sh',
  user: 'alice',
  state: 'T',
  elapsed_seconds: 600,
  command: '/bin/bash /home/alice/backup.sh'
})
const zombie = proc({
  pid: 400,
  ppid: 1,
  name: 'old-proc',
  user: 'bob',
  state: 'Z',
  elapsed_seconds: 120,
  command: '[old-proc]'
})

function telemetry(overrides: Record<string, unknown> = {}) {
  return {
    top_cpu_processes: [worker, database],
    top_mem_processes: [database, worker],
    stopped_processes: [stopped],
    zombie_processes: [zombie],
    process_summary: {
      total: 500,
      running: 3,
      sleeping: 480,
      disk_sleep: 1,
      stopped: 1,
      zombie: 1,
      other: 14,
      threads: 1200
    },
    ...overrides
  }
}

describe('Processes screen', () => {
  afterEach(cleanup)

  it('shows the whole-machine tally, not just the reported rows', () => {
    render(Processes, { props: { device: device(telemetry()) } })
    expect(document.body.textContent).toContain('4 reported of 500 processes')
  })

  it('falls back to the reported subset on an older agent payload', () => {
    const { process_summary, stopped_processes, zombie_processes, ...legacy } = telemetry()
    void process_summary
    void stopped_processes
    void zombie_processes
    render(Processes, { props: { device: device(legacy) } })
    expect(document.body.textContent).toContain('Counts are the reported subset')
  })
})
