<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import Icon from '../ui/Icon.svelte'
  import { apiFetch, apiURL } from '../../lib/api'
  import { type SecurityPosture } from '../../lib/security-posture'
  import { portIdentity } from '../../lib/ports'
  import { formatDuration, formatRelative } from '../../lib/format'
  import type { Device } from '../../lib/types'
  import { nav, type Section } from '../../lib/router'
  import EmptyState from '../ui/EmptyState.svelte'
  import MiniSparkline from '../ui/MiniSparkline.svelte'

  type LynisFinding = { id: string; category: string; description: string; detail?: string; severity?: string }
  type LynisCategory = { name: string; tests: number; passed: number; warnings: number; suggestions: number }
  type LynisReport = {
    hardening_index: number
    total_warnings: number
    total_suggestions: number
    total_tests: number
    tests_passed: number
    lynis_version: string
    os: string
    kernel: string
    warnings: LynisFinding[]
    suggestions: LynisFinding[]
    categories: LynisCategory[]
    audit_date: string
  }
  type SecurityAudit = {
    id: string
    device_id: string
    hardening_index: number
    total_warnings: number
    total_suggestions: number
    total_tests: number
    tests_passed: number
    lynis_version: string
    os_info: string
    kernel_version: string
    report_json: string
    created_at: string
  }
  type FindingKind = 'warning' | 'suggestion'
  type Finding = LynisFinding & { kind: FindingKind }

  export let device: Device
  export let authHeaders: Record<string, string> = {}
  export let lynisAvailable = false
  export let lynisInstallCmd = ''
  // Posture is fetched by the shell (App.svelte), not here: component-local
  // async state was not flushing this component's own render until the next
  // poll, and the shell's state updates reliably.
  export let posture: SecurityPosture = {
    publicPorts: [],
    pendingUpdates: 0,
    pendingStatus: '',
    failedServices: [],
    loaded: false,
    error: ''
  }
  // Derive plain locals in reactive statements. The template's direct read of
  // `posture.publicPorts.length` did not repaint on prop change in this Svelte
  // version, while a reactive statement over the same prop did; the template
  // tracks the locals correctly.
  $: publicPorts = posture.publicPorts
  $: publicPortCount = posture.publicPorts.length
  $: pendingUpdateLabel = posture.pendingStatus === 'ready' ? String(posture.pendingUpdates) : '—'
  $: failedServices = posture.failedServices
  $: failedUnitCount = posture.failedServices.length

  let audits: SecurityAudit[] = []
  let selected: SecurityAudit | null = null
  let report: LynisReport | null = null
  let loading = false
  let error = ''
  let running = false
  let runMessage = ''
  let installCopied = false
  let destroyed = false

  // Finding filters. A 40-finding report is unusable as one flat list, so the
  // reader can narrow by kind, by category and by free text.
  let kindFilter: 'all' | FindingKind = 'all'
  let categoryFilter = ''
  let query = ''

  onMount(() => {
    void loadAudits()
  })

  onDestroy(() => {
    destroyed = true
  })

  async function apiCall(method: string, path: string, body?: any): Promise<any> {
    const opts: RequestInit = { method, headers: { ...authHeaders } }
    if (body) {
      opts.headers = { ...opts.headers, 'Content-Type': 'application/json' }
      opts.body = JSON.stringify(body)
    }
    const res = await apiFetch(apiURL(path), opts)
    if (!res.ok) {
      const err = await res.json().catch(() => ({ error: `${res.status}` }))
      throw new Error(err.error || `${res.status}`)
    }
    if (res.status === 204) return null
    return res.json()
  }

  async function loadAudits() {
    loading = true
    error = ''
    try {
      const list: SecurityAudit[] = await apiCall('GET', `/api/devices/${device.id}/security/audits?limit=30`)
      audits = list || []
      if (audits.length > 0) selectAudit(audits[0])
      else {
        selected = null
        report = null
      }
    } catch (e: any) {
      error = e.message || 'Failed to load audits'
    } finally {
      loading = false
    }
  }

  function selectAudit(audit: SecurityAudit) {
    selected = audit
    kindFilter = 'all'
    categoryFilter = ''
    query = ''
    try {
      report = JSON.parse(audit.report_json)
    } catch {
      report = null
    }
  }

  async function runAudit() {
    running = true
    runMessage = ''
    error = ''
    let cmdId = ''
    try {
      const result = await apiCall('POST', `/api/devices/${device.id}/security/audit`)
      cmdId = result.id
      if (!cmdId) throw new Error('Server did not return an audit command ID')
      runMessage = 'Audit queued. Waiting for the agent…'
      await pollForCompletion(cmdId)
    } catch (e: any) {
      error = e.message || 'Failed to queue audit'
      running = false
    }
  }

  async function pollForCompletion(cmdId: string, maxAttempts = 60) {
    for (let i = 0; i < maxAttempts; i++) {
      if (destroyed) return
      try {
        const command = await apiCall('GET', `/api/devices/${device.id}/commands/${cmdId}`)
        if (command.status === 'completed') {
          await loadAudits()
          runMessage = 'Audit completed and report saved.'
          running = false
          return
        }
        if (command.status === 'failed') {
          error = command.message || 'Lynis audit failed.'
          running = false
          return
        }
        runMessage = 'Audit queued. Waiting for the agent…'
      } catch {
        /* keep polling */
      }
      await new Promise((r) => setTimeout(r, 3000))
    }
    if (!destroyed) {
      running = false
      runMessage = 'Audit is still queued. It will run when the agent reports in.'
    }
  }

  async function deleteAudit(auditId: string) {
    try {
      await apiCall('DELETE', `/api/devices/${device.id}/security/audits/${auditId}`)
      audits = audits.filter((a) => a.id !== auditId)
      if (selected?.id === auditId) {
        if (audits.length > 0) selectAudit(audits[0])
        else {
          selected = null
          report = null
        }
      }
    } catch (e: any) {
      error = e.message || 'Failed to delete audit'
    }
  }

  function scoreTone(score: number): 'ok' | 'warn' | 'crit' {
    if (score >= 75) return 'ok'
    if (score >= 50) return 'warn'
    return 'crit'
  }

  function scoreLabel(score: number): string {
    if (score >= 85) return 'Strongly hardened'
    if (score >= 75) return 'Well hardened'
    if (score >= 65) return 'Moderately hardened'
    if (score >= 50) return 'Lightly hardened'
    return 'Poorly hardened'
  }

  // The findings, flattened and tagged so one list can be filtered three ways.
  $: findings = [
    ...(report?.warnings ?? []).map((f) => ({ ...f, kind: 'warning' as FindingKind })),
    ...(report?.suggestions ?? []).map((f) => ({ ...f, kind: 'suggestion' as FindingKind }))
  ] as Finding[]

  $: categories = Array.from(new Set(findings.map((f) => f.category).filter(Boolean))).sort()

  $: visibleFindings = findings.filter((f) => {
    if (kindFilter !== 'all' && f.kind !== kindFilter) return false
    if (categoryFilter && f.category !== categoryFilter) return false
    if (query) {
      const hay = `${f.id} ${f.category} ${f.description}`.toLowerCase()
      if (!hay.includes(query.toLowerCase())) return false
    }
    return true
  })

  // Hardening index across audits, oldest first, for the trend sparkline. A
  // falling line is the thing a DevOps reader is looking for.
  $: trend = [...audits].reverse().map((a) => a.hardening_index)
  $: trendDelta = trend.length >= 2 ? trend[trend.length - 1] - trend[0] : 0

  $: currentTone = report ? scoreTone(report.hardening_index) : 'ok'

  // Recommended actions, highest-signal first. Each is a real next step in the
  // product (jump to the terminal, the logs, the packages) rather than a
  // fabricated shell command we cannot verify for this host.
  //
  // Built as an inline IIFE rather than a called helper: Svelte's reactive
  // analysis only sees identifiers written in the statement itself, so
  // `$: actions = buildActions()` never re-ran when the posture arrived and the
  // card stayed empty on screen.
  type Action = { tone: 'crit' | 'warn' | 'info'; title: string; detail: string; label: string; section?: Section; href?: string }
  $: actions = (() => {
    const out: Action[] = []
    if (failedUnitCount > 0) {
      out.push({ tone: 'crit', title: `${failedUnitCount} failed systemd unit${failedUnitCount === 1 ? '' : 's'}`, detail: failedServices.slice(0, 3).join(', '), label: 'View logs', section: 'logs' })
    }
    if (publicPortCount > 0) {
      out.push({ tone: 'warn', title: `${publicPortCount} service${publicPortCount === 1 ? '' : 's'} listening publicly`, detail: publicPorts.map((p) => `${p.port}/${p.protocol}`).slice(0, 6).join(' '), label: 'Open terminal', section: 'remote' })
    }
    if (posture.pendingStatus === 'ready' && posture.pendingUpdates > 0) {
      out.push({ tone: 'warn', title: `${posture.pendingUpdates} pending package update${posture.pendingUpdates === 1 ? '' : 's'}`, detail: 'Unpatched packages are the most common finding.', label: 'View packages', section: 'packages' })
    }
    for (const w of (report?.warnings ?? []).slice(0, 3)) {
      out.push({ tone: 'info', title: w.description || w.id, detail: `${w.id}${w.category ? ` · ${w.category}` : ''}`, label: 'Lynis docs', href: docsUrl(w.id) })
    }
    return out
  })()

  function docsUrl(id: string): string {
    return `https://cisofy.com/lynis/controls/${encodeURIComponent(id)}/`
  }

  function goto(section: Section) {
    nav.setSection(section)
  }

  function findingCount(kind: 'all' | FindingKind): number {
    if (kind === 'all') return findings.length
    return findings.filter((f) => f.kind === kind).length
  }

  async function copyInstallCmd() {
    const cmd = lynisInstallCmd || 'sudo apt install lynis'
    try {
      if (navigator.clipboard && window.isSecureContext) await navigator.clipboard.writeText(cmd)
      else {
        const ta = document.createElement('textarea')
        ta.value = cmd
        ta.style.position = 'fixed'
        ta.style.left = '-9999px'
        document.body.appendChild(ta)
        ta.select()
        document.execCommand('copy')
        document.body.removeChild(ta)
      }
      installCopied = true
      setTimeout(() => (installCopied = false), 2000)
    } catch {
      /* clipboard unavailable */
    }
  }
</script>

<section class="security">
  <header class="toolbar">
    <div>
      <h2>Security</h2>
      <p class="sub">Lynis hardening audit and host posture for {device.display_name || device.hostname}</p>
    </div>
    {#if lynisAvailable}
      <button class="run" on:click={runAudit} disabled={running}>{running ? 'Running…' : 'Run audit'}</button>
    {:else}
      <div class="install">
        <span class="install-label">Lynis not installed</span>
        <code>{lynisInstallCmd || 'sudo apt install lynis'}</code>
        <button class="copy" on:click={copyInstallCmd}>{installCopied ? 'Copied' : 'Copy'}</button>
      </div>
    {/if}
  </header>

  {#if runMessage}<div class="banner info" aria-live="polite">{runMessage}</div>{/if}
  {#if error}<div class="banner crit">{error}</div>{/if}

  <!-- The verdict: one line that answers "is this device secure?". -->
  {#if report}
    <div class="verdict {currentTone}">
      <div class="verdict-score">
        <span class="verdict-num">{report.hardening_index}</span><span class="verdict-den">/100</span>
      </div>
      <div class="verdict-text">
        <strong>{scoreLabel(report.hardening_index)}</strong>
        <span>
          {report.tests_passed}/{report.total_tests} tests passed ·
          {report.total_warnings} warning{report.total_warnings === 1 ? '' : 's'} ·
          {report.total_suggestions} suggestion{report.total_suggestions === 1 ? '' : 's'}
          {#if selected}· audited {formatRelative(selected.created_at)}{/if}
        </span>
      </div>
      {#if trend.length >= 2}
        <div class="verdict-trend">
          <MiniSparkline series={trend} color={currentTone === 'crit' ? 'var(--crit)' : currentTone === 'warn' ? 'var(--warn)' : 'var(--accent)'} height={34} label="Hardening index trend" />
          <span class="trend-label" class:up={trendDelta > 0} class:down={trendDelta < 0}>
            {trendDelta > 0 ? '+' : ''}{trendDelta} over {trend.length} audits
          </span>
        </div>
      {/if}
    </div>
  {:else if lynisAvailable}
    <div class="verdict none">
      <div class="verdict-text">
        <strong>No audit yet</strong>
        <span>Run an audit to get a hardening index and the findings behind it.</span>
      </div>
    </div>
  {/if}

  <div class="posture">
    <article class="chip"><span class="label">OS</span><span class="chip-val">{device.hardware?.operating_system || '—'}</span></article>
    <article class="chip"><span class="label">Kernel</span><span class="chip-val mono">{device.hardware?.kernel_version || '—'}</span></article>
    <!-- Keyed on the derived counts: the template's direct read of the posture
         prop did not repaint in this Svelte version, but recreating the block
         when the derived values change does. -->
    {#key publicPortCount + '|' + pendingUpdateLabel + '|' + failedUnitCount}
      <article class="chip"><span class="label">Public listeners</span><span class="chip-val" class:warn={publicPortCount > 0}>{publicPortCount}</span></article>
      <article class="chip"><span class="label">Pending updates</span><span class="chip-val" class:warn={posture.pendingUpdates > 0}>{pendingUpdateLabel}</span></article>
      <article class="chip"><span class="label">Failed units</span><span class="chip-val" class:crit={failedUnitCount > 0}>{failedUnitCount}</span></article>
    {/key}
    <article class="chip"><span class="label">Uptime</span><span class="chip-val">{formatDuration(device.hardware?.uptime_seconds)}</span></article>
    <article class="chip"><span class="label">Last seen</span><span class="chip-val">{formatRelative(device.last_seen_at)}</span></article>
  </div>

  {#if posture.error}
    <div class="banner crit">{posture.error}</div>
  {/if}

  <!-- Exposure detail: the counts above, spelled out with the ports and units.
       Wrapped in a keyed block on the same counts as the chips: in this Svelte
       version the derived counts only propagate into a re-created block, so a
       plain {#if} here stayed false while the chips already showed the values. -->
  {#key publicPortCount + '|' + failedUnitCount}
    {#if publicPortCount > 0 || failedUnitCount > 0}
      <div class="exposure">
        {#if publicPortCount > 0}
          <span class="exposure-title">Public listeners</span>
          <div class="exposure-list">
            {#each publicPorts as p (portIdentity(p))}
              <span class="tag mono">{p.port}/{p.protocol}</span>
            {/each}
          </div>
        {/if}
        {#if failedUnitCount > 0}
          <span class="exposure-title crit">Failed units</span>
          <div class="exposure-list">
            {#each failedServices as unit (unit)}
              <span class="tag mono">{unit}</span>
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  {/key}

  {#key actions.length}
  {#if actions.length > 0}
    <article class="card actions">
      <header class="card-head"><h3>Recommended actions</h3><span class="count">{actions.length}</span></header>
      <ul class="action-list">
        {#each actions as action, i (i)}
          <li class="action {action.tone}">
            <div class="action-body">
              <span class="action-title">{action.title}</span>
              {#if action.detail}<span class="action-detail">{action.detail}</span>{/if}
            </div>
            {#if action.section}
              <button class="action-btn" on:click={() => goto(action.section!)}>{action.label}</button>
            {:else if action.href}
              <a class="action-btn" href={action.href} target="_blank" rel="noopener noreferrer">{action.label}</a>
            {/if}
          </li>
        {/each}
      </ul>
    </article>
  {/if}
  {/key}

  {#if loading}
    <p class="loading">Loading audits…</p>
  {:else}
    <div class="audits">
      <article class="card history">
        <header class="card-head"><h3>Audit history</h3><span class="count">{audits.length}</span></header>
        {#if audits.length === 0}
          <EmptyState icon="shield" title="No security audits yet" message={lynisAvailable ? 'Run an audit to create the first report.' : 'Install Lynis on the device first.'} />
        {:else}
          <ul class="audit-list">
            {#each audits as audit (audit.id)}
              <li class="audit-item">
                <button class="audit-row" class:sel={selected?.id === audit.id} on:click={() => selectAudit(audit)}>
                  <span class="score-dot {scoreTone(audit.hardening_index)}">{audit.hardening_index}</span>
                  <span class="audit-meta">
                    <span class="audit-date">{new Date(audit.created_at).toLocaleString()}</span>
                    <span class="audit-sub">{audit.total_warnings} warnings · {audit.total_suggestions} suggestions</span>
                  </span>
                </button>
                <button class="del" title="Delete audit" aria-label="Delete audit" on:click={() => deleteAudit(audit.id)}><Icon name="x" size={12} /></button>
              </li>
            {/each}
          </ul>
        {/if}
      </article>

      <article class="card report">
        {#if report}
          <header class="report-head">
            <div class="gauge {scoreTone(report.hardening_index)}">
              <span class="gauge-num">{report.hardening_index}</span>
              <span class="gauge-den">/100</span>
            </div>
            <div class="report-meta">
              <strong class="tone-{scoreTone(report.hardening_index)}">{scoreLabel(report.hardening_index)}</strong>
              <span>{report.tests_passed}/{report.total_tests} tests passed</span>
              {#if report.lynis_version}<span class="dim">Lynis {report.lynis_version} · {report.os}</span>{/if}
              {#if report.kernel}<span class="dim mono">{report.kernel}</span>{/if}
            </div>
            <div class="report-counts">
              <span class="warn">{report.total_warnings} warnings</span>
              <span class="info">{report.total_suggestions} suggestions</span>
            </div>
          </header>

          {#if report.categories?.length}
            <section class="findings">
              <h4 class="find-head">By category</h4>
              <div class="cats">
                {#each report.categories as cat}
                  <div class="cat">
                    <span class="cat-name">{cat.name}</span>
                    <div class="cat-bar"><i style="width: {cat.tests > 0 ? (cat.passed / cat.tests * 100) : 0}%"></i></div>
                    <span class="cat-stats">{cat.passed}/{cat.tests}{#if cat.warnings} · <b class="warn">{cat.warnings}W</b>{/if}{#if cat.suggestions} · <b class="info">{cat.suggestions}S</b>{/if}</span>
                  </div>
                {/each}
              </div>
            </section>
          {/if}

          {#if findings.length > 0}
            <section class="findings">
              <h4 class="find-head">Findings</h4>
              <div class="filters">
                <div class="kinds" role="group" aria-label="Finding type">
                  <button class:active={kindFilter === 'all'} on:click={() => (kindFilter = 'all')}>All {findingCount('all')}</button>
                  <button class:active={kindFilter === 'warning'} on:click={() => (kindFilter = 'warning')}>Warnings {findingCount('warning')}</button>
                  <button class:active={kindFilter === 'suggestion'} on:click={() => (kindFilter = 'suggestion')}>Suggestions {findingCount('suggestion')}</button>
                </div>
                {#if categories.length > 1}
                  <select bind:value={categoryFilter} aria-label="Filter by category">
                    <option value="">All categories</option>
                    {#each categories as cat (cat)}<option value={cat}>{cat}</option>{/each}
                  </select>
                {/if}
                <input class="search" type="search" bind:value={query} placeholder="Filter findings…" aria-label="Filter findings" />
              </div>

              {#if visibleFindings.length === 0}
                <p class="none-match">No findings match the current filter.</p>
              {:else}
                <div class="finding-list">
                  {#each visibleFindings as f (`${f.kind}-${f.id}`)}
                    <div class="finding {f.kind}">
                      <span class="fid {f.kind} mono">{f.id}</span>
                      <span class="fcat">{f.category}</span>
                      <span class="fdesc">
                        {f.description}
                        {#if f.detail}<span class="fdetail">{f.detail}</span>{/if}
                      </span>
                      <a class="fdocs" href={docsUrl(f.id)} target="_blank" rel="noopener noreferrer" title={`Lynis control ${f.id}`}>docs ↗</a>
                    </div>
                  {/each}
                </div>
              {/if}
            </section>
          {/if}
        {:else if audits.length > 0}
          <EmptyState icon="file" title="Report unreadable" message="The stored report could not be parsed." />
        {:else}
          <EmptyState icon="shield" title="No report selected" message="Run or select an audit to read its findings." />
        {/if}
      </article>
    </div>
  {/if}
</section>

<style>
  .security { display: grid; gap: 0.85rem; }

  .toolbar { display: flex; align-items: flex-start; justify-content: space-between; gap: 0.75rem; flex-wrap: wrap; }
  .toolbar h2 { margin: 0; font-size: 1rem; color: var(--text-bright); }
  .sub { margin: 0.15rem 0 0; color: var(--text-muted); font-size: 0.76rem; }

  .run { padding: 0.45rem 0.9rem; border: 0; border-radius: var(--radius-sm); background: var(--accent); color: var(--accent-ink); font-weight: 600; font-size: 0.78rem; cursor: pointer; }
  .run:disabled { opacity: 0.6; cursor: default; }
  .install { display: flex; align-items: center; gap: 0.4rem; }
  .install-label { color: var(--warn); font-size: 0.72rem; font-weight: 600; }
  .install code { padding: 0.2rem 0.45rem; background: var(--card-inset); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--accent); font-size: 0.72rem; }
  .copy { padding: 0.2rem 0.5rem; border: 1px solid var(--border); background: transparent; color: var(--text-muted); border-radius: var(--radius-sm); font-size: 0.7rem; cursor: pointer; }

  .banner { padding: 0.5rem 0.75rem; border-radius: var(--radius-sm); font-size: 0.76rem; }
  .banner.info { background: rgba(96, 165, 250, 0.12); border: 1px solid rgba(96, 165, 250, 0.35); color: var(--series-1); }
  .banner.crit { background: var(--crit-dim); border: 1px solid rgba(248, 113, 113, 0.35); color: var(--crit); }

  .verdict { display: flex; align-items: center; gap: 1rem; padding: 0.9rem 1rem; border: 1px solid var(--border); border-left-width: 3px; border-radius: var(--radius); background: var(--card); flex-wrap: wrap; }
  .verdict.ok { border-left-color: var(--accent); }
  .verdict.warn { border-left-color: var(--warn); }
  .verdict.crit { border-left-color: var(--crit); }
  .verdict.none { border-left-color: var(--border-strong); }
  .verdict-score { display: flex; align-items: baseline; gap: 0.1rem; }
  .verdict-num { font-size: 2rem; font-weight: 700; font-variant-numeric: tabular-nums; }
  .verdict.ok .verdict-num { color: var(--accent); }
  .verdict.warn .verdict-num { color: var(--warn); }
  .verdict.crit .verdict-num { color: var(--crit); }
  .verdict-den { color: var(--text-faint); font-size: 0.8rem; }
  .verdict-text { display: grid; gap: 0.15rem; }
  .verdict-text strong { font-size: 1rem; color: var(--text-bright); }
  .verdict-text span { color: var(--text-muted); font-size: 0.76rem; }
  .verdict-trend { margin-left: auto; display: grid; gap: 0.15rem; min-width: 140px; }
  .trend-label { font-size: 0.68rem; color: var(--text-faint); text-align: right; }
  .trend-label.up { color: var(--accent); }
  .trend-label.down { color: var(--crit); }

  .posture { display: grid; grid-template-columns: repeat(auto-fit, minmax(130px, 1fr)); gap: 0.5rem; }
  .chip { display: grid; gap: 0.15rem; padding: 0.6rem 0.7rem; background: var(--card); border: 1px solid var(--border); border-radius: var(--radius-sm); }
  .chip .label { color: var(--text-faint); font-size: 0.66rem; text-transform: uppercase; letter-spacing: 0.04em; }
  .chip-val { color: var(--text-bright); font-size: 0.92rem; font-weight: 500; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .chip-val.warn { color: var(--warn); }
  .chip-val.crit { color: var(--crit); }

  .exposure { display: flex; align-items: center; gap: 0.6rem; flex-wrap: wrap; padding: 0.55rem 0.75rem; background: var(--warn-dim); border: 1px solid rgba(251, 191, 36, 0.3); border-radius: var(--radius-sm); }
  .exposure-title { color: var(--warn); font-size: 0.72rem; font-weight: 600; }
  .exposure-title.crit { color: var(--crit); }
  .exposure-list { display: flex; flex-wrap: wrap; gap: 0.3rem; }
  .tag { padding: 0.1rem 0.4rem; background: var(--card-inset); border: 1px solid var(--border); border-radius: var(--radius-pill); color: var(--text); font-size: 0.68rem; }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .card-head { display: flex; align-items: center; justify-content: space-between; padding: 0.65rem 0.8rem; border-bottom: 1px solid var(--border); }
  .card-head h3 { margin: 0; font-size: 0.78rem; font-weight: 600; color: var(--text); }
  .count { color: var(--text-faint); font-size: 0.7rem; }

  .action-list { list-style: none; margin: 0; padding: 0; }
  .action { display: flex; align-items: center; gap: 0.6rem; padding: 0.55rem 0.8rem; border-bottom: 1px solid var(--border); border-left: 3px solid var(--border); }
  .action:last-child { border-bottom: 0; }
  .action.crit { border-left-color: var(--crit); }
  .action.warn { border-left-color: var(--warn); }
  .action.info { border-left-color: var(--series-1); }
  .action-body { display: grid; gap: 0.1rem; min-width: 0; flex: 1; }
  .action-title { color: var(--text); font-size: 0.78rem; }
  .action-detail { color: var(--text-faint); font-size: 0.7rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .action-btn { flex-shrink: 0; padding: 0.3rem 0.6rem; border: 1px solid var(--border-strong); border-radius: var(--radius-sm); background: var(--card-inset); color: var(--text); font-size: 0.72rem; text-decoration: none; cursor: pointer; }
  .action-btn:hover { border-color: var(--accent); color: var(--accent); }

  .audits { display: grid; grid-template-columns: minmax(240px, 320px) 1fr; gap: 0.85rem; align-items: start; }

  .audit-list { list-style: none; margin: 0; padding: 0; max-height: 64vh; overflow-y: auto; }
  .audit-item { display: flex; align-items: center; border-bottom: 1px solid var(--border); }
  .audit-item:last-child { border-bottom: 0; }
  .audit-row { display: flex; align-items: center; gap: 0.6rem; flex: 1; min-width: 0; padding: 0.55rem 0.3rem 0.55rem 0.8rem; border: 0; background: transparent; color: var(--text); text-align: left; cursor: pointer; }
  .audit-row:hover { background: var(--card-hover); }
  .audit-row.sel { background: var(--accent-dim); box-shadow: inset 2px 0 0 var(--accent); }
  .score-dot { display: grid; place-items: center; width: 34px; height: 34px; border-radius: 50%; font-size: 0.8rem; font-weight: 700; flex-shrink: 0; }
  .score-dot.ok { background: var(--accent-dim); color: var(--accent); }
  .score-dot.warn { background: var(--warn-dim); color: var(--warn); }
  .score-dot.crit { background: var(--crit-dim); color: var(--crit); }
  .audit-meta { display: grid; gap: 0.1rem; min-width: 0; }
  .audit-date { font-size: 0.76rem; color: var(--text); }
  .audit-sub { font-size: 0.68rem; color: var(--text-faint); }
  .del { flex-shrink: 0; border: 0; background: transparent; color: var(--text-faint); cursor: pointer; padding: 0.4rem 0.6rem; }
  .del:hover { color: var(--crit); }

  .report-head { display: flex; align-items: center; gap: 1rem; padding: 1rem; border-bottom: 1px solid var(--border); flex-wrap: wrap; }
  .gauge { display: grid; place-items: center; width: 74px; height: 74px; border-radius: 50%; border: 4px solid var(--border); flex-shrink: 0; }
  .gauge.ok { border-color: var(--accent); }
  .gauge.warn { border-color: var(--warn); }
  .gauge.crit { border-color: var(--crit); }
  .gauge-num { font-size: 1.5rem; font-weight: 700; }
  .gauge.ok .gauge-num { color: var(--accent); }
  .gauge.warn .gauge-num { color: var(--warn); }
  .gauge.crit .gauge-num { color: var(--crit); }
  .gauge-den { font-size: 0.62rem; color: var(--text-faint); }
  .report-meta { display: grid; gap: 0.15rem; font-size: 0.76rem; color: var(--text-muted); }
  .report-meta strong { font-size: 0.95rem; }
  .tone-ok { color: var(--accent); }
  .tone-warn { color: var(--warn); }
  .tone-crit { color: var(--crit); }
  .report-counts { margin-left: auto; display: flex; gap: 0.75rem; font-size: 0.76rem; }
  .dim { color: var(--text-faint); }

  .findings { padding: 0.8rem 1rem; border-bottom: 1px solid var(--border); }
  .findings:last-child { border-bottom: 0; }
  .find-head { margin: 0 0 0.5rem; font-size: 0.7rem; text-transform: uppercase; letter-spacing: 0.06em; color: var(--text-faint); }

  .filters { display: flex; gap: 0.5rem; flex-wrap: wrap; margin-bottom: 0.6rem; align-items: center; }
  .kinds { display: flex; border: 1px solid var(--border); border-radius: var(--radius-sm); overflow: hidden; }
  .kinds button { padding: 0.28rem 0.55rem; border: 0; background: transparent; color: var(--text-faint); font-size: 0.7rem; font-weight: 600; cursor: pointer; }
  .kinds button:hover { background: var(--card-hover); color: var(--text); }
  .kinds button.active { background: var(--accent-dim); color: var(--accent); }
  .filters select, .filters .search { padding: 0.28rem 0.5rem; background: var(--card-inset); border: 1px solid var(--border); border-radius: var(--radius-sm); color: var(--text); font-size: 0.72rem; }
  .filters .search { flex: 1; min-width: 140px; }

  .finding-list { display: grid; }
  .finding { display: flex; gap: 0.5rem; align-items: baseline; padding: 0.32rem 0; font-size: 0.76rem; border-bottom: 1px solid var(--border); }
  .finding:last-child { border-bottom: 0; }
  .fid { font-weight: 600; white-space: nowrap; }
  .fid.warning { color: var(--warn); }
  .fid.suggestion { color: var(--series-1); }
  .fcat { color: var(--text-faint); font-size: 0.68rem; white-space: nowrap; }
  .fdesc { color: var(--text); flex: 1; }
  .fdetail { display: block; color: var(--text-faint); font-size: 0.7rem; margin-top: 0.1rem; }
  .fdocs { color: var(--text-faint); font-size: 0.68rem; text-decoration: none; white-space: nowrap; }
  .fdocs:hover { color: var(--accent); }
  .none-match { margin: 0; color: var(--text-faint); font-size: 0.76rem; padding: 0.3rem 0; }

  .cats { display: grid; grid-template-columns: repeat(auto-fill, minmax(220px, 1fr)); gap: 0.5rem; }
  .cat { display: grid; gap: 0.2rem; }
  .cat-name { font-size: 0.72rem; color: var(--text); }
  .cat-bar { height: 5px; background: var(--card-inset); border-radius: var(--radius-pill); overflow: hidden; }
  .cat-bar i { display: block; height: 100%; background: var(--accent); }
  .cat-stats { font-size: 0.66rem; color: var(--text-faint); }
  .warn { color: var(--warn); }
  .info { color: var(--series-1); }
  .crit { color: var(--crit); }
  .loading { padding: 1rem; color: var(--text-faint); font-size: 0.78rem; }

  @media (max-width: 900px) {
    .audits { grid-template-columns: 1fr; }
  }
</style>
