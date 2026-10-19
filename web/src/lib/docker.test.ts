import { describe, expect, it } from 'vitest'
import {
  containerCounts,
  deriveRates,
  filterContainers,
  filterLogLines,
  friendlyDockerError,
  normalizeLogText,
  normalizeContainer,
  normalizeContainers,
  normalizeDockerState,
  parseComposeFiles,
  parseContainerInspect,
  parseContainerList,
  portLabel,
  portsSummary,
  shortId,
  stateTone,
  statsFromMessage,
  stripLogTimestamps,
  type DockerContainer
} from './docker'

function container(over: Partial<DockerContainer> = {}): DockerContainer {
  return {
    id: '0123456789abcdef',
    name: 'web',
    image: 'nginx:alpine',
    state: 'running',
    status: 'Up 2 hours',
    ports: [],
    mounts: [],
    created_at: 0,
    ...over
  }
}

describe('normalizeContainer', () => {
  it('reads the telemetry shape', () => {
    const c = normalizeContainer({
      id: 'abc123',
      name: 'web',
      image: 'nginx',
      state: 'Running',
      status: 'Up 3 minutes',
      ports: [{ private_port: 80, public_port: 8080, type: 'tcp', ip: '0.0.0.0' }],
      mounts: [{ type: 'bind', source: '/srv/web', destination: '/usr/share/nginx/html', rw: false }]
    })
    expect(c).toMatchObject({ id: 'abc123', name: 'web', state: 'running', image: 'nginx' })
    expect(c?.ports).toEqual([{ private_port: 80, public_port: 8080, type: 'tcp', ip: '0.0.0.0' }])
    expect(c?.mounts?.[0].rw).toBe(false)
  })

  it('reads the list-command shape and strips the leading slash', () => {
    const c = normalizeContainer({ id: 'def456', names: ['/db'], image: 'postgres', state: 'exited', created_at: 1700000000 })
    expect(c?.name).toBe('db')
    expect(c?.created_at).toBe(1700000000)
  })

  it('falls back to the short id when no name exists', () => {
    expect(normalizeContainer({ id: '0123456789abcdef' })?.name).toBe('0123456789ab')
  })

  it('drops entries without an id, because no action could address them', () => {
    expect(normalizeContainer({ name: 'ghost' })).toBeNull()
    expect(normalizeContainers([{ name: 'ghost' }, { id: 'real', name: 'web' }]).map((c) => c.id)).toEqual(['real'])
  })

  it('collapses duplicate IPv4/IPv6 bindings of the same published port', () => {
    const c = normalizeContainer({
      id: 'x',
      ports: [
        { ip: '0.0.0.0', private_port: 8080, public_port: 8099, type: 'tcp' },
        { ip: '::', private_port: 8080, public_port: 8099, type: 'tcp' }
      ]
    })
    expect(c?.ports).toHaveLength(1)
    expect(portLabel(c!.ports[0])).toBe('8099 → 8080/tcp')
  })

  it('ignores malformed ports and mounts instead of failing the row', () => {
    const c = normalizeContainer({
      id: 'x',
      ports: [{ type: 'tcp' }, null, { private_port: 443, type: 'tcp' }],
      mounts: [{ destination: '' }, { destination: '/data', source: '/srv/data' }]
    })
    expect(c?.ports).toHaveLength(1)
    expect(c?.mounts).toHaveLength(1)
  })
})

describe('normalizeDockerState', () => {
  it('treats an absent availability flag as available', () => {
    expect(normalizeDockerState({ containers: [] }).available).toBe(true)
  })

  it('honours an explicit false and keeps the engine info', () => {
    const state = normalizeDockerState({ available: false, containers: [], info: { version: '27.1' } })
    expect(state.available).toBe(false)
    expect(state.info?.version).toBe('27.1')
  })

  it('survives a null payload', () => {
    expect(normalizeDockerState(null)).toEqual({ available: true, containers: [], info: null })
  })
})

describe('containerCounts', () => {
  it('buckets every non-running state as stopped except paused', () => {
    const counts = containerCounts([
      container({ id: '1', state: 'running' }),
      container({ id: '2', state: 'exited' }),
      container({ id: '3', state: 'dead' }),
      container({ id: '4', state: 'paused' })
    ])
    expect(counts).toEqual({ total: 4, running: 1, stopped: 2, paused: 1 })
  })
})

describe('filterContainers', () => {
  const items = [
    container({ id: 'a', name: 'web', image: 'nginx:alpine', state: 'running', ports: [{ private_port: 80, public_port: 8080, type: 'tcp' }] }),
    container({ id: 'b', name: 'db', image: 'postgres:16', state: 'exited', status: 'Exited (0) 2 days ago' }),
    container({ id: 'c', name: 'cache', image: 'redis:7', state: 'paused' })
  ]

  it('returns everything for the default filters', () => {
    expect(filterContainers(items, '', 'all')).toHaveLength(3)
  })

  it('matches the name, image, id and status case-insensitively', () => {
    expect(filterContainers(items, 'POSTGRES', 'all').map((c) => c.name)).toEqual(['db'])
    expect(filterContainers(items, 'cache', 'all').map((c) => c.name)).toEqual(['cache'])
    expect(filterContainers(items, '2 days', 'all').map((c) => c.name)).toEqual(['db'])
  })

  it('matches a published port, which is how users search for a service', () => {
    expect(filterContainers(items, '8080', 'all').map((c) => c.name)).toEqual(['web'])
  })

  it('narrows by state bucket', () => {
    expect(filterContainers(items, '', 'running').map((c) => c.name)).toEqual(['web'])
    expect(filterContainers(items, '', 'paused').map((c) => c.name)).toEqual(['cache'])
    expect(filterContainers(items, '', 'stopped').map((c) => c.name)).toEqual(['db'])
  })
})

describe('portLabel', () => {
  it('reads host to container', () => {
    expect(portLabel({ private_port: 80, public_port: 8080, type: 'tcp' })).toBe('8080 → 80/tcp')
  })

  it('shows only the container port when it is not published', () => {
    expect(portLabel({ private_port: 5432, type: 'tcp' })).toBe('5432/tcp')
  })

  it('summarises the first ports and counts the rest', () => {
    const c = container({
      ports: [
        { private_port: 80, public_port: 8080, type: 'tcp' },
        { private_port: 443, public_port: 8443, type: 'tcp' },
        { private_port: 53, public_port: 5353, type: 'udp' }
      ]
    })
    expect(portsSummary(c)).toBe('8080 → 80/tcp, 8443 → 443/tcp +1')
    expect(portsSummary(container())).toBe('')
  })
})

describe('stateTone', () => {
  it('maps engine states onto the shared status vocabulary', () => {
    expect(stateTone('running')).toBe('ok')
    expect(stateTone('paused')).toBe('warn')
    expect(stateTone('restarting')).toBe('warn')
    expect(stateTone('dead')).toBe('crit')
    expect(stateTone('exited')).toBe('idle')
  })

  it('shortens ids the way the Docker CLI does', () => {
    expect(shortId('0123456789abcdef')).toBe('0123456789ab')
  })
})

describe('parseContainerList', () => {
  it('parses a normal list result', () => {
    const list = parseContainerList('[{"id":"a","names":["/web"],"image":"nginx","state":"running"}]')
    expect(list).toHaveLength(1)
    expect(list[0].name).toBe('web')
  })

  it('treats an empty message as no containers', () => {
    expect(parseContainerList('')).toEqual([])
    expect(parseContainerList(undefined)).toEqual([])
  })

  it('reports unreadable or unexpected bodies instead of pretending the device is empty', () => {
    expect(() => parseContainerList('not json')).toThrow(/unreadable/)
    expect(() => parseContainerList('{"containers":[]}')).toThrow(/unexpected/)
  })
})

describe('parseComposeFiles', () => {
  it('parses the agent compose list', () => {
    const files = parseComposeFiles(
      '[{"path":"/srv/app/compose.yaml","content":"services: {}\\n","size_bytes":42}]'
    )
    expect(files).toEqual([{ path: '/srv/app/compose.yaml', content: 'services: {}\n', size_bytes: 42 }])
  })

  it('skips entries without a path', () => {
    expect(parseComposeFiles('[{"content":"x"},{"path":"/srv/compose.yml"}]')).toHaveLength(1)
  })

  it('reports unreadable or unexpected bodies', () => {
    expect(() => parseComposeFiles('{')).toThrow(/unreadable/)
    expect(() => parseComposeFiles('{}')).toThrow(/unexpected/)
    expect(parseComposeFiles('')).toEqual([])
    // A nil slice marshals as `null` on older agents; that is "none found".
    expect(parseComposeFiles('null')).toEqual([])
  })
})

describe('friendlyDockerError', () => {
  it('explains a local policy denial', () => {
    expect(friendlyDockerError('command disabled by local policy', 'start')).toMatch(/local policy/)
  })

  it('explains a missing Docker socket', () => {
    expect(friendlyDockerError('docker not available')).toMatch(/socket/)
  })

  it('keeps an unknown device reason', () => {
    expect(friendlyDockerError('no such container: deadbeef', 'stop')).toBe(
      'Could not stop: no such container: deadbeef'
    )
  })

  it('says when the device gave no reason at all', () => {
    expect(friendlyDockerError('', 'remove')).toMatch(/no reason/)
  })
})

describe('statsFromMessage', () => {
  it('normalises the stats payload', () => {
    const stats = statsFromMessage(
      JSON.stringify({ cpu_usage_percent: 12.5, memory_percent: '3.2', memory_usage_bytes: 1024, pids: 7 })
    )
    expect(stats.cpu_usage_percent).toBe(12.5)
    expect(stats.memory_percent).toBe(3.2)
    expect(stats.memory_usage_bytes).toBe(1024)
    expect(stats.pids).toBe(7)
  })

  it('rejects empty or malformed stats', () => {
    expect(() => statsFromMessage('')).toThrow(/no container statistics/)
    expect(() => statsFromMessage('nope')).toThrow(/unreadable/)
    expect(() => statsFromMessage('[]')).toThrow(/unexpected/)
  })
})

describe('parseContainerInspect', () => {
  it('normalises the filtered inspect payload', () => {
    const detail = parseContainerInspect(
      JSON.stringify({
        id: 'abc',
        name: '/web',
        image: 'sha256:1',
        platform: 'linux',
        created: '2026-10-18T10:00:00Z',
        restart_policy: 'unless-stopped',
        readonly_rootfs: true,
        memory_limit_bytes: 268435456,
        nano_cpus: 500000000,
        command: ['server', '--token=***'],
        entrypoint: ['/entrypoint.sh'],
        working_dir: '/app',
        user: '1000:1000',
        state: { status: 'running', running: true, pid: 42, exit_code: 0 },
        health: { status: 'healthy', failing_streak: 0 },
        networks: [
          { name: 'alpha', ip_address: '10.0.0.2', gateway: '10.0.0.1', aliases: ['web'] },
          { name: '' }
        ],
        mounts: [{ type: 'bind', source: '/srv', destination: '/app', rw: true }],
        labels: { 'com.example.app': 'web', 'com.example.skip': 42 }
      })
    )
    expect(detail).toMatchObject({
      id: 'abc',
      name: 'web',
      restart_policy: 'unless-stopped',
      readonly_rootfs: true,
      memory_limit_bytes: 268435456,
      nano_cpus: 500000000,
      working_dir: '/app',
      user: '1000:1000'
    })
    expect(detail.state).toMatchObject({ running: true, pid: 42, status: 'running' })
    expect(detail.networks).toHaveLength(1)
    expect(detail.mounts).toHaveLength(1)
    expect(detail.labels).toEqual({ 'com.example.app': 'web' })
    expect(detail.health?.status).toBe('healthy')
  })

  it('defaults a missing restart policy and health', () => {
    const detail = parseContainerInspect(JSON.stringify({ id: 'abc' }))
    expect(detail.restart_policy).toBe('no')
    expect(detail.health).toBeNull()
    expect(detail.networks).toEqual([])
  })

  it('reports missing or malformed detail instead of rendering empty facts', () => {
    expect(() => parseContainerInspect('')).toThrow(/no container detail/)
    expect(() => parseContainerInspect('nope')).toThrow(/unreadable/)
    expect(() => parseContainerInspect('[]')).toThrow(/unexpected/)
  })
})

describe('deriveRates', () => {
  const base = {
    cpu_usage_percent: 1,
    memory_usage_bytes: 10,
    memory_limit_bytes: 100,
    memory_percent: 10,
    network_rx_bytes: 1000,
    network_tx_bytes: 2000,
    block_read_bytes: 3000,
    block_write_bytes: 4000,
    pids: 1
  }

  it('returns zero for the first sample', () => {
    expect(deriveRates(null, base, 2000)).toEqual({ rx_rate: 0, tx_rate: 0, read_rate: 0, write_rate: 0 })
  })

  it('converts counter deltas into per-second rates', () => {
    const next = {
      ...base,
      network_rx_bytes: 3000,
      network_tx_bytes: 3000,
      block_read_bytes: 5000,
      block_write_bytes: 5000
    }
    expect(deriveRates(base, next, 2000)).toEqual({
      rx_rate: 1000,
      tx_rate: 500,
      read_rate: 1000,
      write_rate: 500
    })
  })

  it('treats a counter reset as zero rather than a negative spike', () => {
    const restarted = { ...base, network_rx_bytes: 10, network_tx_bytes: 10, block_read_bytes: 10, block_write_bytes: 10 }
    expect(deriveRates(base, restarted, 2000)).toEqual({ rx_rate: 0, tx_rate: 0, read_rate: 0, write_rate: 0 })
  })

  it('does not divide by a non-positive interval', () => {
    expect(deriveRates(base, base, 0).rx_rate).toBe(0)
  })
})

describe('log helpers', () => {
  const lines = [
    '2026-10-18T12:00:00.123456789Z server listening on :8080',
    '2026-10-18T12:00:01.000000000Z GET /health 200',
    'no timestamp here'
  ].join('\n')

  it('strips the per-line timestamp without touching the message', () => {
    expect(stripLogTimestamps(lines)).toBe(
      ['server listening on :8080', 'GET /health 200', 'no timestamp here'].join('\n')
    )
  })

  it('splits a record that arrived without a trailing newline', () => {
    expect(normalizeLogText('first line2026-10-18T12:00:02Z second')).toBe(
      'first line\n2026-10-18T12:00:02Z second'
    )
  })

  it('filters lines case-insensitively', () => {
    expect(filterLogLines(lines, 'GET')).toBe('2026-10-18T12:00:01.000000000Z GET /health 200')
    expect(filterLogLines(lines, '')).toBe(lines)
  })
})
