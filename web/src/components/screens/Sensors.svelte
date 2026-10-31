<script lang="ts">
  import { formatBytes, formatRate, severityFor, tempSeverity, clampPercent } from '../../lib/format'
  import type { Device } from '../../lib/types'
  import { memoryPercent, swapPercent } from '../../lib/types'
  import EmptyState from '../ui/EmptyState.svelte'
  import SeverityBadge from '../ui/SeverityBadge.svelte'
  import MiniSparkline from '../ui/MiniSparkline.svelte'

  export let device: Device

  const MAX_SAMPLES = 60
  let tempSeries: number[] = []
  let lastStamp = ''
  let seriesDeviceId = ''

  // The shell reuses this component when another device is selected. Without
  // this reset the sparkline would mix samples from two devices.
  function resetSeries(id: string) {
    if (!id || id === seriesDeviceId) return
    seriesDeviceId = id
    tempSeries = []
    lastStamp = ''
  }
  $: resetSeries(device.id)

  type Severity = 'ok' | 'warn' | 'crit'
  type Reading = { group: string; name: string; detail: string; value: string; severity: Severity }

  $: hw = device.hardware
  $: cpuTemp = hw?.cpu_temperature ?? 0
  $: gpuTemp = hw?.gpu_temperature_celsius ?? 0
  $: battery = hw?.battery_percent ?? null
  $: batteryStatus = (hw?.battery_status ?? '').trim()
  $: charging = /charg|full/i.test(batteryStatus)
  // The agent returns (0, "") when the machine has no battery — a desktop. An
  // empty status is the only thing that separates "no battery" from a genuinely
  // empty one, so treat it as absent instead of flagging a 0% CRIT.
  $: batteryPresent = batteryStatus !== '' || (battery != null && battery > 0)
  $: disks = (hw?.disk_partitions ?? []).filter((p) => p.total_bytes > 0).sort((a, b) => b.used_percent - a.used_percent)
  $: ifaces = (hw?.network_ifaces ?? []).filter((i) => i.name !== 'lo')

  // Built inline rather than via a helper: Svelte's reactive analysis only sees
  // identifiers written in the statement, so `$: readings = buildReadings()`
  // never re-ran when the hardware payload changed.
  $: readings = (() => {
    const out: Reading[] = []
    if (cpuTemp > 0) out.push({ group: 'Thermal', name: 'CPU temperature', detail: 'max thermal zone', value: `${cpuTemp.toFixed(0)}°C`, severity: tempSeverity(cpuTemp) })
    if (gpuTemp > 0) out.push({ group: 'Thermal', name: 'GPU temperature', detail: 'drm/hwmon', value: `${gpuTemp.toFixed(0)}°C`, severity: tempSeverity(gpuTemp) })
    if (hw?.power_watts) out.push({ group: 'Power', name: 'Power draw', detail: 'system package', value: `${hw.power_watts.toFixed(1)} W`, severity: 'ok' })
    if (batteryPresent && battery != null) out.push({ group: 'Power', name: 'Battery', detail: batteryStatus || 'no status reported', value: `${battery.toFixed(0)}%`, severity: batterySeverity() })
    const memPct = memoryPercent(hw)
    out.push({ group: 'Memory', name: 'RAM utilisation', detail: `${formatBytes(hw?.memory_used_bytes ?? 0)} of ${formatBytes(hw?.memory_total_bytes ?? 0)}`, value: `${memPct.toFixed(0)}%`, severity: severityFor(memPct) })
    if ((hw?.swap_total_bytes ?? 0) > 0) {
      const sp = swapPercent(hw)
      out.push({ group: 'Memory', name: 'Swap utilisation', detail: `${formatBytes(hw?.swap_used_bytes ?? 0)} of ${formatBytes(hw?.swap_total_bytes ?? 0)}`, value: `${sp.toFixed(0)}%`, severity: severityFor(sp) })
    }
    for (const d of disks) {
      out.push({ group: 'Storage', name: `Filesystem ${d.mount}`, detail: `${formatBytes(d.free_bytes)} free of ${formatBytes(d.total_bytes)}`, value: `${d.used_percent.toFixed(0)}%`, severity: severityFor(d.used_percent) })
    }
    for (const i of ifaces) {
      out.push({
        group: 'Network',
        name: i.name,
        detail: [i.oper_state, i.speed_mbps ? `${i.speed_mbps} Mb/s` : '', (i.addresses ?? [])[0] ?? ''].filter(Boolean).join(' · ') || 'link',
        value: `${formatRate(i.rx_rate || 0)} ↓`,
        severity: i.oper_state && i.oper_state !== 'up' ? 'warn' : 'ok'
      })
    }
    return out
  })()
  $: alerts = readings.filter((r) => r.severity !== 'ok')

  $: if (hw?.collected_at && hw.collected_at !== lastStamp) {
    lastStamp = hw.collected_at
    tempSeries = push(tempSeries, hw.cpu_temperature || 0)
  }

  function push(series: number[], value: number): number[] {
    const out = [...series, value]
    return out.length > MAX_SAMPLES ? out.slice(out.length - MAX_SAMPLES) : out
  }

  function batterySeverity(): Severity {
    if (!batteryPresent || battery == null) return 'ok'
    if (charging) return 'ok'
    if (battery <= 10) return 'crit'
    if (battery <= 20) return 'warn'
    return 'ok'
  }

  function sevLabel(sev: string): 'info' | 'warn' | 'crit' {
    return sev === 'crit' ? 'crit' : sev === 'warn' ? 'warn' : 'info'
  }

  function readingClass(sev: Severity): string {
    return sev === 'crit' ? 'crit' : sev === 'warn' ? 'warn' : ''
  }
</script>

<section class="sensors">
  {#if !hw}
    <EmptyState icon="thermometer" title="No telemetry yet" message="The agent has not reported any sensor readings for this device." />
  {:else}
    <div class="summary">
      <article class="stat">
        <span class="label">CPU temperature</span>
        <span class="value" class:text-warn={tempSeverity(cpuTemp) === 'warn'} class:text-crit={tempSeverity(cpuTemp) === 'crit'}>
          {cpuTemp > 0 ? `${cpuTemp.toFixed(0)}°C` : '—'}
        </span>
        {#if cpuTemp > 0}
          <MiniSparkline series={tempSeries} color="var(--series-3)" height={26} label="CPU temperature" />
        {/if}
      </article>
      <article class="stat">
        <span class="label">GPU temperature</span>
        <span class="value" class:text-warn={tempSeverity(gpuTemp) === 'warn'} class:text-crit={tempSeverity(gpuTemp) === 'crit'}>
          {gpuTemp > 0 ? `${gpuTemp.toFixed(0)}°C` : '—'}
        </span>
        <span class="sub">{gpuTemp > 0 ? 'drm card hwmon' : 'no GPU sensor reported'}</span>
      </article>
      <article class="stat">
        <span class="label">Power draw</span>
        <span class="value">{hw.power_watts ? `${hw.power_watts.toFixed(1)}` : '—'}{#if hw.power_watts}<small>W</small>{/if}</span>
        <span class="sub">instantaneous</span>
      </article>
      <article class="stat">
        <span class="label">Battery</span>
        <span class="value" class:text-warn={batterySeverity() === 'warn'} class:text-crit={batterySeverity() === 'crit'}>
          {batteryPresent && battery != null ? `${battery.toFixed(0)}` : '—'}{#if batteryPresent && battery != null}<small>%</small>{/if}
        </span>
        <span class="sub">{batteryPresent ? (batteryStatus || 'status unknown') : 'no battery (desktop)'}</span>
      </article>
    </div>

    <article class="card">
      <header class="card-head">
        <h2>Health summary</h2>
        <span class="hint">{readings.length} readings · {alerts.length} out of band</span>
      </header>
      {#if alerts.length === 0}
        <p class="all-good">All {readings.length} sensor readings are within their normal band.</p>
      {:else}
        <ul class="alerts">
          {#each alerts as a}
            <li class="alert {readingClass(a.severity)}">
              <SeverityBadge severity={sevLabel(a.severity)} />
              <span class="alert-name">{a.group} · {a.name}</span>
              <span class="alert-val">{a.value}</span>
              <span class="alert-detail">{a.detail}</span>
            </li>
          {/each}
        </ul>
      {/if}
    </article>

    <article class="card">
      <header class="card-head"><h2>All readings</h2><span class="hint">live from the last telemetry payload</span></header>
      <table class="readings">
        <thead><tr><th>Group</th><th>Sensor</th><th>Detail</th><th class="num">Value</th></tr></thead>
        <tbody>
          {#each readings as r}
            <tr>
              <td class="dim">{r.group}</td>
              <td class="mono">{r.name}</td>
              <td class="dim detail">{r.detail}</td>
              <td class="num {readingClass(r.severity)}">{r.value}</td>
            </tr>
          {/each}
        </tbody>
      </table>
    </article>

    <p class="footnote">
      Fan speeds (hwmon fan*_input), per-thermal-zone labels, NVMe/SMART health and GPU utilisation are
      not collected by the agent yet, so those rows are omitted rather than fabricated.
    </p>
  {/if}
</section>

<style>
  .sensors { display: grid; gap: 0.85rem; }

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

  .all-good { margin: 0; padding: 0.9rem; color: var(--ok); font-size: 0.8rem; }
  .alerts { list-style: none; margin: 0; padding: 0.5rem 0.9rem; display: grid; gap: 0.4rem; }
  .alert { display: grid; grid-template-columns: auto 1fr auto; align-items: baseline; gap: 0.5rem; padding: 0.35rem 0; border-bottom: 1px solid var(--border); font-size: 0.78rem; }
  .alert:last-child { border-bottom: none; }
  .alert-name { color: var(--text); }
  .alert-val { font-weight: 600; font-variant-numeric: tabular-nums; }
  .alert-detail { grid-column: 2 / 4; color: var(--text-faint); font-size: 0.7rem; }
  .alert.warn .alert-val { color: var(--warn); }
  .alert.crit .alert-val { color: var(--crit); }

  .readings { width: 100%; border-collapse: collapse; }
  .readings th { text-align: left; padding: 0.5rem 0.9rem; color: var(--text-faint); font-size: 0.68rem; font-weight: 500; text-transform: uppercase; letter-spacing: 0.04em; border-bottom: 1px solid var(--border); }
  .readings td { padding: 0.42rem 0.9rem; border-bottom: 1px solid var(--border); color: var(--text); font-size: 0.78rem; }
  .readings tr:last-child td { border-bottom: none; }
  .readings .num { text-align: right; font-variant-numeric: tabular-nums; }
  .readings .num.warn { color: var(--warn); }
  .readings .num.crit { color: var(--crit); }
  .dim { color: var(--text-muted); }
  .detail { max-width: 22rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }

  .mono { font-family: var(--mono); }
  .footnote { margin: 0; color: var(--text-faint); font-size: 0.7rem; line-height: 1.5; }
</style>
