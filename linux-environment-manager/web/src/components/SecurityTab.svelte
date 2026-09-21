<script lang="ts">
  import { onMount, onDestroy } from 'svelte'
  import { serverBase } from '../lib/api'

  type LynisFinding = {
    id: string
    category: string
    description: string
    severity?: string
  }

  type LynisCategory = {
    name: string
    tests: number
    passed: number
    warnings: number
    suggestions: number
  }

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
    owner_id: string
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

  export let deviceId: string
  export let authHeaders: Record<string, string> = {}
  export let lynisAvailable: boolean = false
  export let lynisInstallCmd: string = ''

  let audits: SecurityAudit[] = []
  let latestReport: LynisReport | null = null
  let loading = false
  let error = ''
  let running = false
  let runMessage = ''
  let expandedAuditId: string | null = null
  let installCopied = false

  async function apiCall(method: string, path: string, body?: any): Promise<any> {
    const opts: RequestInit = { method, headers: { ...authHeaders } }
    if (body) {
      opts.headers = { ...opts.headers, 'Content-Type': 'application/json' }
      opts.body = JSON.stringify(body)
    }
    const res = await fetch(`${serverBase}${path}`, opts)
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
      audits = await apiCall('GET', `/api/devices/${deviceId}/security/audits?limit=20`)
      if (audits && audits.length > 0) {
        parseLatestReport(audits[0])
      } else {
        latestReport = null
      }
    } catch (e: any) {
      error = e.message || 'Failed to load audits'
    } finally {
      loading = false
    }
  }

  function parseLatestReport(audit: SecurityAudit) {
    try {
      latestReport = JSON.parse(audit.report_json)
    } catch {
      latestReport = null
    }
  }

  async function runAudit() {
    running = true
    runMessage = ''
    error = ''
    let queuedCmdId = ''
    try {
      const result = await apiCall('POST', `/api/devices/${deviceId}/security/audit`)
      queuedCmdId = result.id
      runMessage = 'Audit queued. Waiting for agent to complete...'
      // Poll for completion by checking command status AND audit list
      await pollForCompletion(queuedCmdId)
    } catch (e: any) {
      error = e.message || 'Failed to queue audit'
      running = false
    }
  }

  async function pollForCompletion(_cmdId: string, maxAttempts = 60) {
    for (let i = 0; i < maxAttempts; i++) {
      await new Promise(r => setTimeout(r, 3000))
      try {
        // Poll the audit list directly: a completed Lynis run appends a new
        // audit record, which is the authoritative completion signal.
        const newAudits = await apiCall('GET', `/api/devices/${deviceId}/security/audits?limit=1`)
        if (newAudits && newAudits.length > 0) {
          const latest = newAudits[0]
          if (audits.length === 0 || latest.id !== audits[0].id) {
            audits = newAudits.concat(audits)
            parseLatestReport(latest)
            running = false
            runMessage = 'Audit completed!'
            setTimeout(() => { runMessage = '' }, 3000)
            return
          }
        }
      } catch { /* continue polling */ }
    }
    running = false
    runMessage = 'Audit timed out. The agent may be offline or using an older version.'
  }

  async function deleteAudit(auditId: string) {
    try {
      await apiCall('DELETE', `/api/devices/${deviceId}/security/audits/${auditId}`)
      audits = audits.filter(a => a.id !== auditId)
      if (expandedAuditId === auditId) expandedAuditId = null
      if (audits.length > 0) parseLatestReport(audits[0])
      else latestReport = null
    } catch (e: any) {
      error = e.message || 'Failed to delete audit'
    }
  }

  function scoreColor(score: number): string {
    if (score >= 75) return '#22c55e'
    if (score >= 50) return '#f59e0b'
    return '#ef4444'
  }

  function scoreLabel(score: number): string {
    if (score >= 85) return 'Strongly Hardened'
    if (score >= 75) return 'Well Hardened'
    if (score >= 65) return 'Moderately Hardened'
    if (score >= 50) return 'Lightly Hardened'
    return 'Poorly Hardened'
  }

  function formatDate(dateStr: string): string {
    try {
      return new Date(dateStr).toLocaleString()
    } catch {
      return dateStr
    }
  }

  function toggleAudit(auditId: string) {
    expandedAuditId = expandedAuditId === auditId ? null : auditId
  }

  async function copyInstallCmd() {
    const cmd = lynisInstallCmd || 'sudo apt install lynis'
    if (!cmd) return
    try {
      if (navigator.clipboard && window.isSecureContext) {
        await navigator.clipboard.writeText(cmd)
      } else {
        const ta = document.createElement('textarea')
        ta.value = cmd
        ta.style.position = 'fixed'
        ta.style.left = '-9999px'
        document.body.appendChild(ta)
        ta.focus()
        ta.select()
        document.execCommand('copy')
        document.body.removeChild(ta)
      }
      installCopied = true
      setTimeout(() => { installCopied = false }, 2000)
    } catch (e) {
      console.error('copy failed', e)
    }
  }

  onMount(() => {
    loadAudits()
  })
</script>

<div class="security-tab">
  <div class="header-row">
    <h3>Security Audit</h3>
    {#if lynisAvailable}
      <button class="btn-run" on:click={runAudit} disabled={running}>
        {running ? 'Running...' : 'Run Audit'}
      </button>
    {:else}
      <div class="install-notice">
        <span class="install-label">Lynis not installed</span>
        <div class="install-cmd-row">
          <code class="install-cmd">{lynisInstallCmd}</code>
          <button class="btn-copy" on:click={copyInstallCmd} title="Copy to clipboard">
            {installCopied ? 'Copied!' : 'Copy'}
          </button>
        </div>
      </div>
    {/if}
  </div>

  {#if runMessage}
    <div class="info-banner">{runMessage}</div>
  {/if}

  {#if error}
    <div class="error-banner">{error}</div>
  {/if}

  {#if loading}
    <div class="loading">Loading audits...</div>
  {:else if latestReport}
    <!-- Score Card -->
    <div class="score-card">
      <div class="gauge">
        <svg viewBox="0 0 120 80" class="gauge-svg">
          <path d="M 10 70 A 50 50 0 0 1 110 70" fill="none" stroke="#374151" stroke-width="10" stroke-linecap="round"/>
          <path d="M 10 70 A 50 50 0 0 1 110 70" fill="none" stroke={scoreColor(latestReport.hardening_index)}
                stroke-width="10" stroke-linecap="round"
                stroke-dasharray="{latestReport.hardening_index * 1.57} 157"/>
        </svg>
        <div class="gauge-value" style="color: {scoreColor(latestReport.hardening_index)}">
          {latestReport.hardening_index}
        </div>
        <div class="gauge-label">/ 100</div>
      </div>
      <div class="score-info">
        <div class="score-label" style="color: {scoreColor(latestReport.hardening_index)}">
          {scoreLabel(latestReport.hardening_index)}
        </div>
        <div class="score-details">
          <span class="detail-item warning">Warnings: {latestReport.total_warnings}</span>
          <span class="detail-item suggestion">Suggestions: {latestReport.total_suggestions}</span>
          <span class="detail-item passed">Tests: {latestReport.tests_passed}/{latestReport.total_tests} passed</span>
        </div>
        {#if latestReport.lynis_version}
          <div class="score-meta">Lynis {latestReport.lynis_version} | {latestReport.os}</div>
        {/if}
      </div>
    </div>

    <!-- Warnings -->
    {#if latestReport.warnings && latestReport.warnings.length > 0}
      <div class="section">
        <h4 class="section-title warning">Warnings ({latestReport.warnings.length})</h4>
        <div class="findings-list">
          {#each latestReport.warnings as w}
            <div class="finding-item">
              <span class="finding-id">{w.id}</span>
              <span class="finding-cat">[{w.category}]</span>
              <span class="finding-desc">{w.description}</span>
            </div>
          {/each}
        </div>
      </div>
    {/if}

    <!-- Suggestions -->
    {#if latestReport.suggestions && latestReport.suggestions.length > 0}
      <div class="section">
        <h4 class="section-title suggestion">Suggestions ({latestReport.suggestions.length})</h4>
        <div class="findings-list">
          {#each latestReport.suggestions as s}
            <div class="finding-item">
              <span class="finding-id">{s.id}</span>
              <span class="finding-cat">[{s.category}]</span>
              <span class="finding-desc">{s.description}</span>
            </div>
          {/each}
        </div>
      </div>
    {/if}

    <!-- Categories -->
    {#if latestReport.categories && latestReport.categories.length > 0}
      <div class="section">
        <h4 class="section-title">By Category</h4>
        <div class="categories-grid">
          {#each latestReport.categories as cat}
            <div class="category-item">
              <div class="cat-name">{cat.name}</div>
              <div class="cat-bar">
                <div class="cat-bar-fill" style="width: {cat.tests > 0 ? (cat.passed / cat.tests * 100) : 0}%"></div>
              </div>
              <div class="cat-stats">
                {cat.passed}/{cat.tests} passed
                {#if cat.warnings > 0}<span class="cat-warn">{cat.warnings}W</span>{/if}
                {#if cat.suggestions > 0}<span class="cat-sug">{cat.suggestions}S</span>{/if}
              </div>
            </div>
          {/each}
        </div>
      </div>
    {/if}
  {:else if audits.length === 0}
    <div class="empty-state">
      <p>No security audits yet.</p>
      {#if lynisAvailable}
        <p class="hint">Click "Run Audit" to perform a Lynis security scan.</p>
      {:else}
        <p class="hint">Lynis must be installed on this device before running an audit.</p>
        <div class="install-hint">
          <code>{lynisInstallCmd}</code>
          <button class="btn-copy small" on:click={copyInstallCmd}>
            {installCopied ? 'Copied!' : 'Copy'}
          </button>
        </div>
      {/if}
    </div>
  {/if}

  <!-- History -->
  {#if audits.length > 0}
    <div class="section">
      <h4 class="section-title">History</h4>
      <div class="history-list">
        {#each audits as audit}
          <div class="history-item" class:expanded={expandedAuditId === audit.id}>
            <div class="history-row" on:click={() => toggleAudit(audit.id)}>
              <span class="history-date">{formatDate(audit.created_at)}</span>
              <span class="history-score" style="color: {scoreColor(audit.hardening_index)}">
                Score: {audit.hardening_index}
              </span>
              <span class="history-warnings">{audit.total_warnings}W</span>
              <span class="history-suggestions">{audit.total_suggestions}S</span>
              <span class="history-tests">{audit.tests_passed}/{audit.total_tests}</span>
              <button class="btn-delete" on:click|stopPropagation={() => deleteAudit(audit.id)} title="Delete">x</button>
            </div>
            {#if expandedAuditId === audit.id}
              <div class="history-detail">
                {#if audit.lynis_version}<div>Lynis: {audit.lynis_version}</div>{/if}
                {#if audit.os_info}<div>OS: {audit.os_info}</div>{/if}
                {#if audit.kernel_version}<div>Kernel: {audit.kernel_version}</div>{/if}
              </div>
            {/if}
          </div>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
  .security-tab { padding: 8px 0; }
  .header-row { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
  .header-row h3 { margin: 0; font-size: 14px; color: #e5e7eb; }
  .btn-run {
    padding: 6px 16px; border-radius: 6px; border: none; cursor: pointer;
    background: #3b82f6; color: white; font-size: 13px; font-weight: 500;
  }
  .btn-run:hover { background: #2563eb; }
  .btn-run:disabled { background: #4b5563; cursor: not-allowed; }

  .install-notice {
    display: flex; flex-direction: column; align-items: flex-end; gap: 4px;
  }
  .install-label { font-size: 11px; color: #f59e0b; font-weight: 500; }
  .install-cmd-row { display: flex; align-items: center; gap: 6px; }
  .install-cmd {
    padding: 3px 8px; background: #1f2937; border: 1px solid #374151;
    border-radius: 4px; font-size: 11px; color: #60a5fa; font-family: monospace;
  }
  .btn-copy {
    padding: 3px 8px; border-radius: 4px; border: 1px solid #374151;
    background: #374151; color: #e5e7eb; font-size: 11px; cursor: pointer;
  }
  .btn-copy:hover { background: #4b5563; }
  .btn-copy.small { font-size: 10px; padding: 2px 6px; }

  .install-hint {
    display: flex; align-items: center; gap: 8px; margin-top: 8px;
  }
  .install-hint code {
    padding: 4px 8px; background: #1f2937; border-radius: 4px;
    font-size: 12px; color: #60a5fa;
  }

  .info-banner {
    background: #1e3a5f; border: 1px solid #3b82f6; border-radius: 6px;
    padding: 8px 12px; margin-bottom: 8px; color: #93c5fd; font-size: 13px;
  }
  .error-banner {
    background: #3b1111; border: 1px solid #ef4444; border-radius: 6px;
    padding: 8px 12px; margin-bottom: 8px; color: #fca5a5; font-size: 13px;
  }
  .loading { color: #9ca3af; font-size: 13px; padding: 16px 0; text-align: center; }

  .score-card {
    display: flex; gap: 20px; align-items: center; padding: 16px;
    background: #111827; border-radius: 8px; margin-bottom: 12px;
  }
  .gauge { position: relative; width: 100px; height: 70px; flex-shrink: 0; }
  .gauge-svg { width: 100%; height: 100%; }
  .gauge-value {
    position: absolute; top: 30px; left: 50%; transform: translateX(-50%);
    font-size: 22px; font-weight: 700;
  }
  .gauge-label {
    position: absolute; top: 50px; left: 50%; transform: translateX(-50%);
    font-size: 10px; color: #6b7280;
  }
  .score-info { flex: 1; }
  .score-label { font-size: 16px; font-weight: 600; margin-bottom: 6px; }
  .score-details { display: flex; gap: 12px; flex-wrap: wrap; }
  .detail-item { font-size: 12px; color: #9ca3af; }
  .detail-item.warning { color: #f59e0b; }
  .detail-item.suggestion { color: #60a5fa; }
  .detail-item.passed { color: #34d399; }
  .score-meta { font-size: 11px; color: #6b7280; margin-top: 4px; }

  .section { margin-bottom: 12px; }
  .section-title {
    font-size: 12px; font-weight: 600; color: #9ca3af; text-transform: uppercase;
    margin: 0 0 6px 0; letter-spacing: 0.5px;
  }
  .section-title.warning { color: #f59e0b; }
  .section-title.suggestion { color: #60a5fa; }

  .findings-list { display: flex; flex-direction: column; gap: 4px; }
  .finding-item {
    font-size: 12px; padding: 4px 8px; background: #1f2937; border-radius: 4px;
    display: flex; gap: 6px; align-items: baseline;
  }
  .finding-id { color: #f59e0b; font-weight: 600; font-family: monospace; white-space: nowrap; }
  .finding-cat { color: #6b7280; font-size: 11px; white-space: nowrap; }
  .finding-desc { color: #d1d5db; }

  .categories-grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(200px, 1fr)); gap: 6px; }
  .category-item {
    padding: 6px 8px; background: #1f2937; border-radius: 4px;
  }
  .cat-name { font-size: 12px; font-weight: 600; color: #e5e7eb; margin-bottom: 4px; }
  .cat-bar { height: 4px; background: #374151; border-radius: 2px; overflow: hidden; margin-bottom: 3px; }
  .cat-bar-fill { height: 100%; background: #22c55e; border-radius: 2px; transition: width 0.3s; }
  .cat-stats { font-size: 10px; color: #9ca3af; }
  .cat-warn { color: #f59e0b; margin-left: 6px; }
  .cat-sug { color: #60a5fa; margin-left: 4px; }

  .empty-state { text-align: center; padding: 24px 0; color: #9ca3af; }
  .empty-state p { margin: 4px 0; font-size: 13px; }
  .hint { font-size: 12px !important; color: #6b7280; }
  code {
    display: inline-block; margin-top: 8px; padding: 4px 8px; background: #1f2937;
    border-radius: 4px; font-size: 12px; color: #60a5fa;
  }

  .history-list { display: flex; flex-direction: column; gap: 4px; }
  .history-item {
    background: #1f2937; border-radius: 4px; overflow: hidden;
  }
  .history-row {
    display: flex; gap: 12px; align-items: center; padding: 6px 8px;
    cursor: pointer; font-size: 12px;
  }
  .history-row:hover { background: #374151; }
  .history-date { color: #9ca3af; flex-shrink: 0; }
  .history-score { font-weight: 600; }
  .history-warnings { color: #f59e0b; }
  .history-suggestions { color: #60a5fa; }
  .history-tests { color: #6b7280; margin-left: auto; }
  .btn-delete {
    background: none; border: none; color: #6b7280; cursor: pointer;
    font-size: 11px; padding: 2px 4px; border-radius: 3px;
  }
  .btn-delete:hover { color: #ef4444; background: #374151; }
  .history-detail {
    padding: 4px 8px 8px; font-size: 11px; color: #6b7280;
    border-top: 1px solid #374151;
  }
</style>
