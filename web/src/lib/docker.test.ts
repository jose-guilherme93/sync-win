import { describe, expect, it } from 'vitest'
import {
  containerCounts,
  filterContainers,
  friendlyDockerError,
  normalizeContainer,
  normalizeContainers,
  normalizeDockerState,
  parseComposeFiles,
  parseContainerList,
  portLabel,
  portsSummary,
  shortId,
  stateTone,
  statsFromMessage,
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
