<script lang="ts">
  import { formatBytes, formatDuration, severityFor, tempSeverity, clampPercent } from '../../lib/format'
  import type { Device } from '../../lib/types'
  import EmptyState from '../ui/EmptyState.svelte'
  import SeverityBadge from '../ui/SeverityBadge.svelte'
  import MiniSparkline from '../ui/MiniSparkline.svelte'

  export let device: Device

  const MAX_SAMPLES = 60
  let cpuSeries: number[] = []
  let tempSeries: number[] = []
  let lastStamp = ''
  let seriesDeviceId = ''

  // The shell reuses this component when another device is selected. Without
  // this reset the sparklines would mix samples from two devices.
  function resetSeries(id: string) {
    if (!id || id === seriesDeviceId) return
    seriesDeviceId = id
    cpuSeries = []
    tempSeries = []
    lastStamp = ''
  }
  $: resetSeries(device.id)

  $: hw = device.hardware
  $: cores = hw?.cpu_core_usage ?? []
  $: coreCount = cores.length
  $: loads = (hw?.load_average ?? '')
    .trim()
    .split(/\s+/)
    .map((n) => Number.parseFloat(n))
    .filter((n) => Number.isFinite(n))
  $: load1 = loads[0] ?? null
  $: load5 = loads[1] ?? null
  $: load15 = loads[2] ?? null
  // Load is only meaningful relative to core count: 4.0 is idle on 16 cores and
  // saturated on 2. This is the number a DevOps reader actually wants.
  $: loadPerCore = load1 != null && coreCount > 0 ? load1 / coreCount : null
  $: loadSeverity = loadPerCore == null ? 'ok' : loadPerCore >= 1 ? 'crit' : loadPerCore >= 0.7 ? 'warn' : 'ok'
  $: temp = hw?.cpu_temperature ?? 0
  $: clockMhz = hw?.cpu_clock_mhz ?? 0
  $: clockValue = clockMhz >= 1000 ? (clockMhz / 1000).toFixed(2) : clockMhz.toFixed(0)
  $: clockUnit = clockMhz >= 1000 ? 'GHz' : 'MHz'
  $: coreVoltage = hw?.core_voltage_v ?? 0
  $: gpuTemp = hw?.gpu_temperature_celsius ?? 0
  $: procs = (hw?.top_cpu_processes ?? []).slice(0, 10)
  $: busiest = cores.length > 0 ? Math.max(...cores) : null

  $: if (hw?.collected_at && hw.collected_at !== lastStamp) {
    lastStamp = hw.collected_at
    cpuSeries = push(cpuSeries, hw.cpu_usage_percent || 0)
    tempSeries = push(tempSeries, hw.cpu_temperature || 0)
  }

  function push(series: number[], value: number): number[] {
    const out = [...series, value]
    return out.length > MAX_SAMPLES ? out.slice(out.length - MAX_SAMPLES) : out
  }

  function barClass(percent: number): string {
    const sev = severityFor(percent)
    return sev === 'crit' ? 'crit' : sev === 'warn' ? 'warn' : ''
  }

  function sevLabel(sev: string): 'info' | 'warn' | 'crit' {
    return sev === 'crit' ? 'crit' : sev === 'warn' ? 'warn' : 'info'
  }
</script>

<section class="cpu">
  {#if !hw}
    <EmptyState icon="🧮" title="No telemetry yet" message="The agent has not reported CPU or thermal metrics for this device." />
  {:else}
    <div class="summary">
      <article class="stat">
        <span class="label">CPU usage</span>
        <span class="value">{clampPercent(hw.cpu_usage_percent).toFixed(0)}<small>%</small></span>
        <MiniSparkline series={cpuSeries} color="var(--accent)" height={26} label="CPU usage" />
      </article>
      <article class="stat">
        <span class="label">CPU temperature</span>
        <span class="value" class:text-warn={tempSeverity(temp) === 'warn'} class:text-crit={tempSeverity(temp) === 'crit'}>
          {temp > 0 ? `${temp.toFixed(0)}°C` : '—'}
        </span>
        {#if temp > 0}
          <MiniSparkline series={tempSeries} color="var(--series-3)" height={26} label="CPU temperature" />
        {:else}
          <span class="sub">no thermal zone reported</span>
        {/if}
      </article>
      <article class="stat">
        <span class="label">CPU clock</span>
        <span class="value">
          {#if clockMhz > 0}{clockValue}<small>{clockUnit}</small>{:else}—{/if}
        </span>
        <span class="sub">{clockMhz > 0 ? 'average across logical CPUs' : 'no cpufreq or cpu MHz reported'}</span>
      </article>
      <article class="stat">
        <span class="label">Core voltage</span>
        <span class="value">
          {#if coreVoltage > 0}{coreVoltage.toFixed(3)}<small>V</small>{:else}—{/if}
        </span>
        <span class="sub">{coreVoltage > 0 ? 'hwmon CPU rail' : 'no CPU voltage sensor'}</span>
      </article>
      <article class="stat">
        <span class="label">Load (1 min)</span>
        <span class="value" class:text-warn={loadSeverity === 'warn'} class:text-crit={loadSeverity === 'crit'}>
          {load1 != null ? load1.toFixed(2) : '—'}
        </span>
        <span class="sub">
          {#if loadPerCore != null}{loadPerCore.toFixed(2)} per core · {coreCount} cores{:else}no core count{/if}
        </span>
      </article>
      <article class="stat">
        <span class="label">Power draw</span>
        <span class="value">{hw.power_watts ? `${hw.power_watts.toFixed(1)}` : '—'}{#if hw.power_watts}<small>W</small>{/if}</span>
        <span class="sub">{hw.architecture || '—'} · up {formatDuration(hw.uptime_seconds)}</span>
      </article>
    </div>

    <article class="card">
      <header class="card-head">
        <h2>Per-core usage</h2>
        <span class="hint">
          {#if coreCount > 0}{coreCount} cores · busiest {busiest?.toFixed(0)}%{:else}not reported{/if}
        </span>
      </header>
      {#if coreCount === 0}
        <EmptyState icon="🧩" title="Per-core usage not reported" message="The agent did not include cpu_core_usage in the last telemetry payload." />
      {:else}
        <div class="cores">
          {#each cores as usage, i}
            <div class="core">
              <span class="core-id">CPU{i}</span>
              <div class="bar"><i class={barClass(usage)} style="width: {Math.min(usage, 100)}%"></i></div>
              <span class="core-val">{usage.toFixed(0)}%</span>
            </div>
          {/each}
        </div>
      {/if}
    </article>

    <div class="two-col">
      <article class="card">
        <header class="card-head"><h2>Load average</h2></header>
        <div class="loads">
          {#each [{ label: '1 min', v: load1 }, { label: '5 min', v: load5 }, { label: '15 min', v: load15 }] as row}
            <div class="load">
              <span class="load-label">{row.label}</span>
              <span class="load-val">{row.v != null ? row.v.toFixed(2) : '—'}</span>
              <span class="load-sub">{row.v != null && coreCount > 0 ? `${(row.v / coreCount).toFixed(2)} /core` : ''}</span>
            </div>
          {/each}
        </div>
        {#if loadSeverity !== 'ok'}
          <p class="load-note">
            <SeverityBadge severity={sevLabel(loadSeverity)} />
            Run queue exceeds core count — the host is oversubscribed.
          </p>
        {/if}
      </article>

      <article class="card">
        <header class="card-head"><h2>Processor</h2></header>
        <dl class="kv">
          <div><dt>Model</dt><dd class="mono">{hw.cpu_model || '—'}</dd></div>
          <div><dt>Architecture</dt><dd>{hw.architecture || '—'}</dd></div>
          <div><dt>Cores</dt><dd>{coreCount > 0 ? coreCount : '—'}</dd></div>
          <div><dt>Clock</dt><dd>{clockMhz > 0 ? `${clockValue} ${clockUnit}` : '—'}</dd></div>
          <div><dt>Core voltage</dt><dd>{coreVoltage > 0 ? `${coreVoltage.toFixed(3)} V` : '—'}</dd></div>
          <div><dt>Kernel</dt><dd class="mono">{hw.kernel_version || '—'}</dd></div>
          <div><dt>OS</dt><dd>{hw.operating_system || '—'}</dd></div>
          <div><dt>GPU temperature</dt><dd>{gpuTemp > 0 ? `${gpuTemp.toFixed(0)}°C` : '—'}</dd></div>
        </dl>
      </article>
    </div>

    <article class="card">
      <header class="card-head">
        <h2>Top processes by CPU</h2>
        <span class="hint">{procs.length} of {hw.top_cpu_processes?.length ?? 0} shown</span>
      </header>
      {#if procs.length === 0}
        <EmptyState icon="📋" title="No process snapshot" message="The agent did not report top CPU processes." />
      {:else}
        <table class="procs">
          <thead><tr><th>PID</th><th>Process</th><th class="num">CPU</th><th class="num">RSS</th></tr></thead>
          <tbody>
            {#each procs as p (p.pid)}
              <tr>
                <td class="mono dim">{p.pid}</td>
                <td class="mono">{p.name}</td>
                <td class="num">{p.cpu_percent.toFixed(1)}%</td>
                <td class="num dim">{formatBytes(p.mem_rss_bytes)}</td>
              </tr>
            {/each}
          </tbody>
        </table>
      {/if}
    </article>

    <article class="card agent">
      <header class="card-head"><h2>Agent overhead</h2></header>
      <div class="agent-row">
        <span>sync-win-agent</span>
        <span class="mono">{(hw.agent_cpu_usage ?? 0).toFixed(2)}% CPU · {formatBytes(hw.agent_memory_bytes ?? 0)} RSS</span>
      </div>
    </article>

    <p class="footnote">
      Per-core frequency (the clock above is the average across logical CPUs), CPU throttling counters
      and per-thermal-zone labels are not collected by the agent yet, so those rows are omitted rather
      than fabricated.
    </p>
  {/if}
</section>

<style>
  .cpu { display: grid; gap: 0.85rem; }

  .summary { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 0.6rem; }
  .stat {
    display: grid; gap: 0.25rem; align-content: start;
    padding: 0.8rem 0.9rem; background: var(--card);
    border: 1px solid var(--border); border-radius: var(--radius);
  }
  .stat .label { color: var(--text-faint); font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.04em; }
  .stat .value { font-size: 1.45rem; font-weight: 600; color: var(--text-bright); font-variant-numeric: tabular-nums; }
  .stat .value small { font-size: 0.8rem; color: var(--text-muted); font-weight: 400; }
  .stat .sub { color: var(--text-faint); font-size: 0.72rem; }
  .text-warn { color: var(--warn); }
  .text-crit { color: var(--crit); }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); }
  .card-head { display: flex; align-items: baseline; justify-content: space-between; gap: 0.5rem; padding: 0.7rem 0.9rem; border-bottom: 1px solid var(--border); }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .hint { color: var(--text-faint); font-size: 0.7rem; }

  .cores { display: grid; grid-template-columns: repeat(auto-fit, minmax(150px, 1fr)); gap: 0.5rem 0.9rem; padding: 0.9rem; }
  .core { display: grid; grid-template-columns: 2.4rem 1fr 2.4rem; align-items: center; gap: 0.5rem; }
  .core-id { color: var(--text-faint); font-size: 0.68rem; font-family: var(--mono); }
  .core-val { color: var(--text-muted); font-size: 0.72rem; text-align: right; font-variant-numeric: tabular-nums; }

  .bar { height: 8px; border-radius: var(--radius-pill); background: rgba(148, 163, 184, 0.15); overflow: hidden; }
  .bar i { display: block; height: 100%; background: var(--accent); border-radius: inherit; transition: width var(--t); }
  .bar i.warn { background: var(--warn); }
  .bar i.crit { background: var(--crit); }

  .two-col { display: grid; grid-template-columns: repeat(auto-fit, minmax(260px, 1fr)); gap: 0.85rem; }

  .loads { display: grid; gap: 0.5rem; padding: 0.9rem; }
  .load { display: grid; grid-template-columns: 3rem 1fr auto; align-items: baseline; gap: 0.5rem; }
  .load-label { color: var(--text-faint); font-size: 0.72rem; }
  .load-val { font-size: 1.1rem; font-weight: 600; color: var(--text-bright); font-variant-numeric: tabular-nums; }
  .load-sub { color: var(--text-faint); font-size: 0.7rem; }
  .load-note { display: flex; align-items: center; gap: 0.4rem; margin: 0; padding: 0 0.9rem 0.9rem; color: var(--warn); font-size: 0.72rem; }

  .kv { display: grid; gap: 0.45rem; margin: 0; padding: 0.9rem; }
  .kv div { display: flex; justify-content: space-between; gap: 0.6rem; align-items: baseline; }
  .kv dt { color: var(--text-faint); font-size: 0.72rem; }
  .kv dd { margin: 0; color: var(--text); font-size: 0.78rem; text-align: right; overflow: hidden; text-overflow: ellipsis; }

  .procs { width: 100%; border-collapse: collapse; }
  .procs th { text-align: left; padding: 0.5rem 0.9rem; color: var(--text-faint); font-size: 0.68rem; font-weight: 500; text-transform: uppercase; letter-spacing: 0.04em; border-bottom: 1px solid var(--border); }
  .procs td { padding: 0.42rem 0.9rem; border-bottom: 1px solid var(--border); color: var(--text); font-size: 0.78rem; }
  .procs tr:last-child td { border-bottom: none; }
  .procs .num { text-align: right; font-variant-numeric: tabular-nums; }
  .dim { color: var(--text-muted); }

  .agent-row { display: flex; justify-content: space-between; gap: 0.6rem; padding: 0.8rem 0.9rem; color: var(--text-muted); font-size: 0.78rem; }

  .mono { font-family: var(--mono); }
  .footnote { margin: 0; color: var(--text-faint); font-size: 0.7rem; line-height: 1.5; }
</style>
