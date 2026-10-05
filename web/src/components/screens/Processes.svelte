<script lang="ts">
  import { formatBytes } from '../../lib/format'
  import type { Device, ProcessInfo } from '../../lib/types'
  import EmptyState from '../ui/EmptyState.svelte'

  // Processes come straight from the agent payload (top CPU / top memory).
  // There is no per-process history endpoint, so this is a snapshot by design.
  export let device: Device

  let sort: 'cpu' | 'mem' = 'cpu'

  $: hw = device.hardware
  $: cpuProcs = hw?.top_cpu_processes ?? []
  $: memProcs = hw?.top_mem_processes ?? []
  $: list = sort === 'cpu' ? cpuProcs : memProcs
  $: maxCpu = Math.max(1, ...list.map((p) => p.cpu_percent))
  $: totalMem = hw?.memory_total_bytes || 0
</script>

<section class="procs">
  <article class="card">
    <header class="card-head">
      <h2>Top processes</h2>
      <div class="toggle" role="group" aria-label="Sort processes">
        <button class:active={sort === 'cpu'} on:click={() => (sort = 'cpu')}>By CPU</button>
        <button class:active={sort === 'mem'} on:click={() => (sort = 'mem')}>By memory</button>
      </div>
    </header>

    {#if !hw}
      <p class="muted pad">No telemetry yet.</p>
    {:else if list.length === 0}
      <EmptyState
        icon="⚙"
        title="No process data"
        message="The agent did not report a process list for this collection cycle. It is sampled from the top consumers, so an idle machine can legitimately report none."
      />
    {:else}
      <div class="table-scroll">
        <table>
          <thead>
            <tr>
              <th class="num">PID</th>
              <th>Name</th>
              <th class="num">CPU</th>
              <th class="num">Memory</th>
              <th class="bar-col">Share</th>
            </tr>
          </thead>
          <tbody>
            {#each list as proc (proc.pid + proc.name)}
              <tr>
                <td class="num mono">{proc.pid}</td>
                <td class="name" title={proc.name}>{proc.name}</td>
                <td class="num">{proc.cpu_percent.toFixed(1)}%</td>
                <td class="num">
                  {formatBytes(proc.mem_rss_bytes)}
                  {#if totalMem}<span class="pct">{((proc.mem_rss_bytes / totalMem) * 100).toFixed(1)}%</span>{/if}
                </td>
                <td class="bar-col">
                  <div class="bar"><i style="width: {Math.min((proc.cpu_percent / maxCpu) * 100, 100)}%"></i></div>
                </td>
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
      <p class="footnote">
        Snapshot from the last collection cycle. The bar shows each process relative to the busiest
        one in this list, not a share of total CPU.
      </p>
    {/if}
  </article>
</section>

<style>
  .procs { display: grid; gap: 0.85rem; }
  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .card-head { display: flex; align-items: center; justify-content: space-between; gap: 0.5rem; padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }

  .toggle { display: flex; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; }
  .toggle button { padding: 0.25rem 0.5rem; border: 0; background: transparent; color: var(--text-faint); font-size: 0.68rem; font-weight: 600; cursor: pointer; }
  .toggle button:hover { background: var(--card-hover); color: var(--text); }
  .toggle button.active { background: var(--accent-dim); color: var(--accent); }

  .muted { color: var(--text-faint); font-size: 0.8rem; }
  .pad { padding: 1rem 0.9rem; }

  .table-scroll { overflow-x: auto; }
  table { width: 100%; border-collapse: collapse; font-size: 0.78rem; }
  th { text-align: left; padding: 0.5rem 0.7rem; color: var(--text-faint); font-size: 10px; font-weight: 700; letter-spacing: 0.07em; text-transform: uppercase; border-bottom: 1px solid var(--border); white-space: nowrap; }
  td { padding: 0.45rem 0.7rem; border-bottom: 1px solid var(--border); color: var(--text); vertical-align: middle; }
  .num { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; }
  .mono { font-family: var(--mono); color: var(--text-muted); }
  .name { max-width: 22rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--text-bright); }
  .pct { color: var(--text-faint); font-size: 0.68rem; margin-left: 0.3rem; }

  .bar-col { width: 30%; min-width: 80px; }
  .bar { height: 6px; border-radius: var(--radius-pill); background: rgba(148, 163, 184, 0.15); overflow: hidden; }
  .bar i { display: block; height: 100%; background: var(--accent); border-radius: inherit; }

  .footnote { margin: 0; padding: 0.6rem 0.9rem 0.8rem; color: var(--text-faint); font-size: 0.7rem; line-height: 1.5; }
</style>
