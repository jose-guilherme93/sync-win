<script lang="ts">
  import { computedStatus, deviceLabel, deviceTags, memoryPercent, type Device, type DiskPartition } from '../../lib/types'
  import { formatRelative } from '../../lib/format'
  import { THRESHOLDS, generateInsights, type Insight } from '../../lib/insights'
  import StatusDot from '../ui/StatusDot.svelte'
  import SeverityBadge from '../ui/SeverityBadge.svelte'
  import Skeleton from '../ui/Skeleton.svelte'

  export let devices: Device[] = []
  export let onSelect: (id: string) => void
  export let onAdd: () => void
  // Provided by the shell; the summary alert count links to the Alerts screen.
  export let onAlerts: () => void = () => {}
  // The first fetch has not answered yet. An empty list and an unanswered list
  // are different states, and only one of them means "install an agent".
  export let loading = false

  type Row = {
    device: Device
    status: string
    insights: Insight[]
    cpu: number | null
    memory: number | null
    disk: number | null
    diskMount: string
  }

  function primaryDisk(device: Device): DiskPartition | null {
    const parts = (device.hardware?.disk_partitions ?? []).filter((part) => part.total_bytes > 0)
    // Prefer the root filesystem. Containers report the host disk under
    // /etc/hosts (a bind mount) instead of /, so the largest partition is the
    // fallback that still shows something useful there.
    return parts.find((part) => part.mount === '/')
      ?? parts.reduce<DiskPartition | null>((largest, part) =>
        !largest || part.total_bytes > largest.total_bytes ? part : largest, null)
  }

  // Critical devices sort first: the whole point of the fleet view is to make
  // the thing that is on fire the first thing you see.
  $: rows = devices
    .map((device): Row => {
      const hw = device.hardware
      const disk = primaryDisk(device)
      return {
        device,
        status: computedStatus(device),
        insights: generateInsights(hw),
        cpu: hw ? hw.cpu_usage_percent : null,
        memory: hw ? memoryPercent(hw) : null,
        disk: disk ? disk.used_percent : null,
        diskMount: disk?.mount || ''
      }
    })
    .sort((a, b) => {
      const rank = (r: Row) => {
        if (r.insights.some((i) => i.type === 'crit')) return 0
        if (r.status === 'error') return 1
        if (r.status === 'offline') return 2
        if (r.status === 'stale') return 3
        if (r.insights.length > 0) return 4
        return 5
      }
      return rank(a) - rank(b) || deviceLabel(a.device).localeCompare(deviceLabel(b.device))
    })

  $: online = rows.filter((row) => row.status === 'online').length
  $: alertCount = rows.reduce((count, row) => count + row.insights.length, 0)

  // The bar colour is the same decision the insights make, so a device never
  // reads green on the fleet while its detail page raises an alert.
  function tone(value: number | null, warn: number, crit: number): '' | 'warn' | 'crit' {
    if (value == null) return ''
    if (value > crit) return 'crit'
    if (value > warn) return 'warn'
    return ''
  }

  function pct(value: number | null): string {
    return value == null ? '—' : `${value.toFixed(0)}%`
  }
</script>

<section class="home">
  <header class="fleet-bar">
    <p class="summary">
      <b>{rows.length}</b> device{rows.length === 1 ? '' : 's'}
      <span class="sep">·</span>
      <span class:text-ok={online > 0}>{online} online</span>
      <span class="sep">·</span>
      {#if alertCount > 0}
        <button class="alert-link" on:click={onAlerts}>
          {alertCount} alert{alertCount === 1 ? '' : 's'} <span aria-hidden="true">›</span>
        </button>
      {:else}
        <span class="quiet">all clear</span>
      {/if}
    </p>
    <button class="add" on:click={onAdd}>+ Add device</button>
  </header>

  {#if loading && rows.length === 0}
    <Skeleton variant="list" rows={3} />
  {:else if rows.length === 0}
    <div class="empty">
      <strong>No devices yet</strong>
      <p>Install the agent on a Linux machine. It appears here within seconds of the installer finishing.</p>
    </div>
  {:else}
    <ul class="devices">
      {#each rows as row (row.device.id)}
        <li>
          <button class="device" on:click={() => onSelect(row.device.id)}>
            <span class="state"><StatusDot status={row.status} label={false} /></span>
            <span class="ident">
              <span class="hostname">{deviceLabel(row.device)}</span>
              <span class="meta">
                <span class="os">{row.device.hardware?.operating_system || '—'}</span>
                <span class="seen">{formatRelative(row.device.last_seen_at)}</span>
                {#if deviceTags(row.device).length > 0}
                  <span class="tags">
                    {#each deviceTags(row.device).slice(0, 2) as tag (tag)}<span class="tag">{tag}</span>{/each}
                  </span>
                {/if}
              </span>
            </span>

            <span class="usage">
              <span class="usage-item" class:warn={tone(row.cpu, THRESHOLDS.cpuWarn, THRESHOLDS.cpuCrit) === 'warn'} class:crit={tone(row.cpu, THRESHOLDS.cpuWarn, THRESHOLDS.cpuCrit) === 'crit'}>
                <span class="usage-top"><span>CPU</span><b>{pct(row.cpu)}</b></span>
                <span class="usage-track"><i style="width: {Math.min(row.cpu ?? 0, 100)}%"></i></span>
              </span>
              <span class="usage-item" class:warn={tone(row.memory, THRESHOLDS.ramWarn, THRESHOLDS.ramCrit) === 'warn'} class:crit={tone(row.memory, THRESHOLDS.ramWarn, THRESHOLDS.ramCrit) === 'crit'}>
                <span class="usage-top"><span>RAM</span><b>{pct(row.memory)}</b></span>
                <span class="usage-track"><i style="width: {Math.min(row.memory ?? 0, 100)}%"></i></span>
              </span>
              <span class="usage-item" class:warn={tone(row.disk, THRESHOLDS.diskWarn, THRESHOLDS.diskCrit) === 'warn'} class:crit={tone(row.disk, THRESHOLDS.diskWarn, THRESHOLDS.diskCrit) === 'crit'}>
                <span class="usage-top"><span title={row.diskMount || 'largest partition'}>Disk</span><b>{pct(row.disk)}</b></span>
                <span class="usage-track"><i style="width: {Math.min(row.disk ?? 0, 100)}%"></i></span>
              </span>
            </span>

            <span class="health">
              {#if row.insights.length === 0}
                <span class="ok-chip">ok</span>
              {:else}
                <SeverityBadge severity={row.insights[0].type} />
                {#if row.insights.length > 1}<span class="more">+{row.insights.length - 1}</span>{/if}
              {/if}
            </span>
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</section>

<style>
  .home { display: grid; gap: 0.6rem; }

  .fleet-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.75rem;
  }

  .summary {
    margin: 0;
    color: var(--text-muted);
    font-size: 0.78rem;
  }
  .summary b { color: var(--text-bright); }
  .summary .sep { color: var(--text-faint); padding: 0 0.25rem; }
  .quiet { color: var(--text-faint); }

  .alert-link {
    padding: 0;
    border: 0;
    background: transparent;
    color: var(--warn);
    font-size: inherit;
    font-weight: 600;
    cursor: pointer;
  }
  .alert-link:hover { text-decoration: underline; }

  .add {
    padding: 0.3rem 0.6rem;
    border: 1px solid var(--accent-border);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--accent);
    font-size: 0.72rem;
    font-weight: 600;
    cursor: pointer;
  }
  .add:hover { background: var(--accent-dim); }

  .devices {
    list-style: none;
    margin: 0;
    padding: 0;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
  }

  .devices li + li { border-top: 1px solid var(--border); }

  .device {
    display: grid;
    grid-template-columns: auto minmax(0, 1.1fr) minmax(0, 1.8fr) auto;
    gap: 1rem;
    align-items: center;
    width: 100%;
    padding: 0.65rem 0.9rem;
    border: 0;
    background: transparent;
    color: var(--text);
    text-align: left;
    cursor: pointer;
    transition: background var(--t-fast);
  }
  .device:hover { background: var(--card-hover); }

  .state { display: flex; }

  .ident {
    display: grid;
    gap: 0.15rem;
    min-width: 0;
  }
  .hostname {
    color: var(--text-bright);
    font-weight: 600;
    font-size: 0.85rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.35rem;
    color: var(--text-faint);
    font-size: 0.7rem;
  }
  .os { color: var(--text-muted); }
  .tags { display: flex; gap: 0.2rem; }
  .tag {
    padding: 0.02rem 0.35rem;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-pill);
    color: var(--text-faint);
    font-size: 0.6rem;
  }

  .usage {
    display: grid;
    grid-template-columns: repeat(3, minmax(64px, 1fr));
    gap: 0.85rem;
    min-width: 0;
  }
  .usage-item { display: grid; gap: 0.25rem; min-width: 0; }
  .usage-top {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 0.4rem;
    color: var(--text-faint);
    font-size: 0.66rem;
  }
  .usage-top b {
    color: var(--text);
    font-size: 0.7rem;
    font-variant-numeric: tabular-nums;
  }
  .usage-track {
    display: block;
    height: 0.7rem;
    border-radius: 0;
    background: var(--card-inset);
    overflow: hidden;
  }
  .usage-track i {
    display: block;
    height: 100%;
    border-radius: 0;
    /* Solid, muted fill: mixed with the surface instead of the saturated
       accent, so the bars read as matte rather than neon. */
    background: color-mix(in srgb, var(--accent) 65%, var(--card));
  }
  .usage-item.warn .usage-track i { background: color-mix(in srgb, var(--warn) 65%, var(--card)); }
  .usage-item.warn .usage-top b { color: var(--warn); }
  .usage-item.crit .usage-track i { background: color-mix(in srgb, var(--crit) 65%, var(--card)); }
  .usage-item.crit .usage-top b { color: var(--crit); }

  .health {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 0.25rem;
    min-width: 3.5rem;
  }
  .ok-chip {
    color: var(--ok);
    font-size: 0.68rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }
  .more { color: var(--text-faint); font-size: 0.68rem; }

  .empty {
    padding: 2rem 1rem;
    text-align: center;
    color: var(--text-muted);
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }
  .empty strong { color: var(--text); display: block; margin-bottom: 0.25rem; }
  .empty p { margin: 0; font-size: 0.78rem; }

  @media (max-width: 860px) {
    .device {
      grid-template-columns: auto minmax(0, 1fr) auto;
      grid-template-areas:
        'state ident health'
        'usage usage usage';
      row-gap: 0.55rem;
    }
    .state { grid-area: state; }
    .ident { grid-area: ident; }
    .health { grid-area: health; }
    .usage { grid-area: usage; }
  }
</style>
