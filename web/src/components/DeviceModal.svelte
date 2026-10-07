<script lang="ts">
  import { createEventDispatcher, onMount } from 'svelte'
  import SystemMetrics from './SystemMetrics.svelte'
  import DockerTab from './DockerTab.svelte'
  import SecurityTab from './SecurityTab.svelte'
  import DeviceLogs from './screens/DeviceLogs.svelte'
  import { apiFetch, apiURL, serverBase } from '../lib/api'
  // Shared shapes so a Device passed from the shell type-checks against this
  // component. The fields this view reads are a subset of HardwareStats.
  import type { Device, HardwareStats } from '../lib/types'
  import { formatRelative } from '../lib/format'

  type AppInfo = { name: string; version: string; source: string; path?: string }

  type PreferenceFile = {
    id: string
    device_id: string
    user_id: string
    category: string
    filename: string
    relative_path: string
    content: string
    encoding?: string
    content_hash: string
    size_bytes: number
    synced_at: string
    status: string
  }

  type SaveGroup = {
    key: string
    label: string
    prefixId?: string
    files: PreferenceFile[]
  }

  export let device: Device
  export let open = false
  export let authHeaders: Record<string, string> = {}
  export let initialFiles: PreferenceFile[] = []
  // 'modal' is the legacy overlay. 'page' renders the same content inline as
  // the main panel area, which is how the redesigned shell shows a device
  // without opening a popup. The only structural difference is the wrapper.
  export let variant: 'modal' | 'page' = 'modal'
  // Two-way so the shell can drive which tab a sidebar section maps onto.
  export let activeTabName: string | null = null

  const dispatch = createEventDispatcher()
  // Attachment previews use the HttpOnly session cookie; do not put bearer
  // tokens in URLs where they can leak through history or proxy logs.
  const attAuth = ''

  const TAB_NAMES = ['system', 'files', 'apps', 'saves', 'logs', 'docker', 'security', 'notes'] as const
  type TabName = (typeof TAB_NAMES)[number]

  const STATUS_HINTS: Record<string, string> = {
    online: 'Agent reported within the last 30 seconds',
    stale: 'No report for over 30 seconds',
    offline: 'No report for more than 5 minutes',
    error: 'The agent reported a sync failure'
  }

  const appSources = [
    { id: 'aur', label: 'AUR' },
    { id: 'pacman', label: 'Pacman' },
    { id: 'flatpak', label: 'Flatpak' },
    { id: 'apt', label: 'APT' },
    { id: 'appimage', label: 'AppImage' }
  ]

  const APPS_RENDER_LIMIT = 80
  const PANEL_TTL_MS = 60000

  let activeTab: TabName = 'system'
  // A sidebar section can force a tab (e.g. "Containers" -> docker). Kept in
  // sync rather than replacing activeTab so the in-page tablist still works.
  $: if (activeTabName && TAB_NAMES.includes(activeTabName as TabName) && activeTab !== activeTabName) {
    activeTab = activeTabName as TabName
  }
  let files: PreferenceFile[] = []
  let apps: AppInfo[] = []
  let saves: PreferenceFile[] = []

  $: telemetry = (device.hardware || {}) as Record<string, any>

  // Findings are computed once per telemetry change and shown above the tabs so
  // the operator sees problems before choosing a tab. Same thresholds the
  // dashboard cards use, so the two never disagree.
  type Finding = { text: string; type: 'info' | 'warn' | 'crit' }

  // device is an explicit argument rather than a closure read so the reactive
  // statement below tracks it. Closing over it meant a new last_error with
  // unchanged telemetry left the findings stale.
  function buildInsights(h: Record<string, any>, dev: { last_error?: string; last_seen_at?: string }): Finding[] {
    const out: Finding[] = []
    const memTotal = h.memory_total_bytes || 0
    const memPct = memTotal ? ((h.memory_used_bytes || 0) / memTotal) * 100 : 0
    const cpu = h.cpu_usage_percent || 0
    const load1 = parseFloat((h.load_average || '').split(' ')[0] || '0')
    const netErrors = Array.isArray(h.network_ifaces)
      ? h.network_ifaces.reduce((acc: number, i: any) => acc + (i.rx_errors || 0) + (i.tx_errors || 0), 0)
      : 0

    if (dev.last_error) out.push({ text: `Sync error: ${dev.last_error}`, type: 'crit' })
    if (cpu >= 90) out.push({ text: `CPU ${cpu.toFixed(0)}%`, type: 'crit' })
    else if (cpu >= 70) out.push({ text: `CPU ${cpu.toFixed(0)}%`, type: 'warn' })
    if (memPct >= 90) out.push({ text: `Memory ${memPct.toFixed(0)}%`, type: 'crit' })
    else if (memPct >= 75) out.push({ text: `Memory ${memPct.toFixed(0)}%`, type: 'warn' })
    if (h.cpu_temperature >= 85) out.push({ text: `CPU ${h.cpu_temperature}°C`, type: 'crit' })
    else if (h.cpu_temperature >= 70) out.push({ text: `CPU ${h.cpu_temperature}°C`, type: 'warn' })
    if (h.gpu_temperature_celsius >= 85) out.push({ text: `GPU ${h.gpu_temperature_celsius}°C`, type: 'crit' })
    if (h.battery_percent > 0 && h.battery_percent < 15 && h.battery_status !== 'charging') {
      out.push({ text: `Battery ${h.battery_percent.toFixed(0)}%`, type: 'crit' })
    }
    if (h.swap_total_bytes > 0) {
      const swapPct = ((h.swap_used_bytes || 0) / h.swap_total_bytes) * 100
      if (swapPct >= 50) out.push({ text: `Swap ${swapPct.toFixed(0)}%`, type: 'warn' })
    }
    for (const p of h.disk_partitions || []) {
      if (p.used_percent >= 90) out.push({ text: `${p.mount} ${p.used_percent.toFixed(0)}% full`, type: 'crit' })
      else if (p.used_percent >= 80) out.push({ text: `${p.mount} ${p.used_percent.toFixed(0)}%`, type: 'warn' })
    }
    if (netErrors > 0) out.push({ text: `${netErrors} network errors`, type: 'warn' })
    // Load above the core count means runnable work is queued behind others.
    if (load1 > 0) {
      const cores = Array.isArray(h.cpu_core_usage) ? h.cpu_core_usage.length : 0
      if (cores > 0 && load1 > cores) out.push({ text: `Load ${load1} over ${cores} cores`, type: 'warn' })
    }
    if ((h.agent_cpu_usage || 0) > 5) out.push({ text: `Agent ${h.agent_cpu_usage.toFixed(1)}% CPU`, type: 'warn' })
    if (h.docker_info?.stopped > 0) out.push({ text: `${h.docker_info.stopped} stopped containers`, type: 'info' })
    if (h.lynis_available === false) out.push({ text: 'Lynis not installed', type: 'info' })
    const age = (Date.now() - Date.parse(h.collected_at || dev.last_seen_at || '')) / 1000
    if (Number.isFinite(age) && age > 120) out.push({ text: 'Telemetry is stale', type: 'warn' })

    return out
  }

  $: insights = buildInsights(telemetry, device)

  
  let filesLoading = false
  let appsLoading = false
  let savesLoading = false
  let appsError = ''
  let savesError = ''
  let appsLoadedAt = 0
  let appQuery = ''
  let expandedGroups: Record<string, boolean> = {}
  let selectedFile: PreferenceFile | null = null
  let toastMessage = ''
  let toastKind: 'success' | 'error' = 'success'
  let toastTimer = 0
  let now = Date.now()
  let clock = 0

  type DeviceNote = { id: string; device_id: string; owner_id: string; content: string; created_at: string; updated_at: string }
  type DeviceAttachment = { id: string; device_id: string; owner_id: string; filename: string; mime_type: string; size_bytes: number; caption?: string; created_at: string }
  let notes: DeviceNote[] = []
  let attachments: DeviceAttachment[] = []
  let notesLoading = false
  let noteContent = ''
  let noteSaving = false
  let editingNoteId: string | null = null
  let editNoteContent = ''
  let openNoteMenuId: string | null = null
  let uploadUploading = false
  let uploadNoteContent = ''
  let uploadCaption = ''

  function close() {
    dispatch('close')
  }

  function statusColor(status: string) {
    if (status === 'online') return 'green'
    if (status === 'offline' || status === 'stale') return 'orange'
    if (status === 'error') return 'red'
    return 'gray'
  }

  // Client-side status fallback: compute status from last_seen_at
  function computedStatus(device: { status: string; last_seen_at: string }): string {
    if (device.status === 'error' || device.status === 'duplicate') return device.status
    const age = (Date.now() - Date.parse(device.last_seen_at)) / 1000
    if (age > 300) return 'offline'  // 5 minutes
    if (age > 30) return 'stale'     // 30 seconds
    return 'online'
  }

  // Delegates to lib/format so a null or Go zero timestamp renders "never".
  const timeAgo = formatRelative

  function formatTime(value: string | undefined | null) {
    if (!value) return ''
    const parsed = new Date(value)
    return Number.isNaN(parsed.getTime()) ? '' : parsed.toLocaleString()
  }

  function formatBytes(value: number) {
    if (!Number.isFinite(value) || value <= 0) return '0 B'
    const units = ['B', 'KB', 'MB', 'GB', 'TB']
    const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
    return `${(value / 1024 ** index).toFixed(index === 0 ? 0 : 1)} ${units[index]}`
  }

  function formatRate(value: number) {
    return `${formatBytes(value)}/s`
  }

  function formatDuration(value: number) {
    const days = Math.floor(value / 86400)
    const hours = Math.floor((value % 86400) / 3600)
    const minutes = Math.floor((value % 3600) / 60)
    return days > 0 ? `${days}d ${hours}h` : `${hours}h ${minutes}m`
  }

  function memoryPercent(h?: HardwareStats) {
    if (!h || !h.memory_total_bytes) return 0
    return (h.memory_used_bytes / h.memory_total_bytes) * 100
  }

  function meterClass(percent: number) {
    if (percent >= 85) return 'crit'
    if (percent >= 60) return 'warn'
    return ''
  }

  function categoryChip(category: string) {
    let hash = 0
    for (let i = 0; i < category.length; i++) hash = (hash * 31 + category.charCodeAt(i)) >>> 0
    const hue = hash % 360
    return `background: hsl(${hue}, 45%, 16%); border-color: hsl(${hue}, 55%, 32%); color: hsl(${hue}, 85%, 80%)`
  }

  function notify(message: string, kind: 'success' | 'error' = 'success') {
    toastMessage = message
    toastKind = kind
    window.clearTimeout(toastTimer)
    toastTimer = window.setTimeout(() => (toastMessage = ''), kind === 'error' ? 4200 : 2400)
  }

  async function copyText(value: string) {
    try {
      await navigator.clipboard.writeText(value)
      notify('Copied to clipboard.')
    } catch {
      notify('Clipboard unavailable; copy manually.', 'error')
    }
  }

  function setTab(tab: TabName) {
    activeTab = tab
    if (tab === 'apps') ensureApps()
    if (tab === 'saves') ensureSaves()
    if (tab === 'notes') {
      void loadNotes()
      void loadAttachments()
    }
    // The logs tab renders DeviceLogs, which loads and polls its own data.
  }

  function tabKeydown(event: KeyboardEvent) {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
    event.preventDefault()
    const index = TAB_NAMES.indexOf(activeTab)
    const next = event.key === 'ArrowRight' ? (index + 1) % TAB_NAMES.length : (index - 1 + TAB_NAMES.length) % TAB_NAMES.length
    setTab(TAB_NAMES[next])
  }

  async function loadFiles() {
    filesLoading = true
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}`), {
        headers: authHeaders
      })
      if (!response.ok) throw new Error(`${response.status}`)
      const payload = await response.json()
      files = Array.isArray(payload) ? payload : []
    } catch {
      files = []
      notify('Could not load synced files', 'error')
    } finally {
      filesLoading = false
    }
  }

  async function loadApps() {
    appsLoading = true
    appsError = ''
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/apps`), {
        headers: authHeaders
      })
      if (!response.ok) throw new Error(`${response.status}`)
      const payload = await response.json()
      if (!Array.isArray(payload)) throw new Error('invalid response')
      apps = payload
      appsLoadedAt = Date.now()
      localStorage.setItem(`sync-win-apps-${device.id}`, JSON.stringify(payload))
    } catch (err) {
      appsError = err instanceof Error ? err.message : 'Could not load app inventory'
      const cached = localStorage.getItem(`sync-win-apps-${device.id}`)
      if (!apps.length && cached) {
        try { apps = JSON.parse(cached) } catch { /* ignore */ }
      }
    } finally {
      appsLoading = false
    }
  }

  async function loadSaves() {
    savesLoading = true
    savesError = ''
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}`), {
        headers: authHeaders
      })
      if (!response.ok) throw new Error(`${response.status}`)
      const payload = await response.json()
      if (!Array.isArray(payload)) throw new Error('invalid response')
      saves = payload.filter((f: PreferenceFile) => f.category === 'saves')
    } catch (err) {
      savesError = err instanceof Error ? err.message : 'Could not load save files'
    } finally {
      savesLoading = false
    }
  }

  function ensureApps() {
    if (!apps.length || Date.now() - appsLoadedAt > PANEL_TTL_MS) {
      void loadApps()
    }
  }

  function ensureSaves() {
    if (!saves.length) {
      void loadSaves()
    }
  }

  function appsFor(source: string) {
    const q = appQuery.trim().toLowerCase()
    return apps.filter((app) => app.source === source && (!q || `${app.name} ${app.version}`.toLowerCase().includes(q)))
  }

  function visibleApps(source: string) {
    const all = appsFor(source)
    const key = source
    if (appQuery.trim() || expandedGroups[key] || all.length <= APPS_RENDER_LIMIT) return all
    return all.slice(0, APPS_RENDER_LIMIT)
  }

  async function reinstallApp(app: AppInfo) {
    if (app.source === 'appimage') {
      notify('AppImages are local files; reinstall is unavailable without a downloadable source.', 'error')
      return
    }
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/apps?action=install`), {
        method: 'POST', headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ source: app.source, name: app.name })
      })
      if (!response.ok) throw new Error(`${response.status}`)
      notify(`${app.name} queued for reinstall on the next agent cycle.`)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not queue reinstall', 'error')
    }
  }

  async function excludeFile(file: PreferenceFile) {
    if (!window.confirm(`Exclude ${file.filename} from synchronization?`)) return
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/files/${file.id}`), {
        method: 'DELETE',
        headers: authHeaders
      })
      if (!response.ok) throw new Error(`${response.status}`)
      notify('File removed. The agent will apply the exclusion on its next cycle.')
      await loadFiles()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not exclude file', 'error')
    }
  }

  async function deleteDevice() {
    if (!window.confirm(`Remove ${device.hostname} and all synchronized data?`)) return
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}`), { method: 'DELETE', headers: authHeaders })
      if (!response.ok) throw new Error(`${response.status}`)
      dispatch('removed')
      close()
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not remove device', 'error')
    }
  }

  function openFileModal(file: PreferenceFile) {
    selectedFile = file
  }

  function closeFileModal() {
    selectedFile = null
  }

  function handleBackdropKeydown(event: KeyboardEvent) {
    if (event.key === 'Escape') {
      if (selectedFile) closeFileModal()
      else close()
    }
  }

  function groupSavesByPrefix(fileList: PreferenceFile[]): SaveGroup[] {
    const groups = new Map<string, SaveGroup>()
    for (const file of fileList) {
      const parts = file.relative_path.split('/')
      const wpIdx = parts.indexOf('wine-prefixes')
      if (wpIdx >= 0 && wpIdx + 1 < parts.length) {
        const prefixId = parts[wpIdx + 1]
        const rest = parts.slice(wpIdx + 2).join('/')
        let gameName = ''
        const docsIdx = rest.indexOf('Documents/')
        if (docsIdx >= 0) {
          const afterDocs = rest.slice(docsIdx + 'Documents/'.length)
          const segs = afterDocs.split('/')
          if (segs.length >= 2) gameName = `${segs[0]}/${segs[1]}`
          else if (segs.length === 1) gameName = segs[0]
        }
        const roamIdx = rest.indexOf('AppData/Roaming/')
        if (!gameName && roamIdx >= 0) {
          const afterRoam = rest.slice(roamIdx + 'AppData/Roaming/'.length)
          const seg = afterRoam.split('/')[0]
          if (seg && seg !== 'Microsoft') gameName = seg
        }
        const lowIdx = rest.indexOf('AppData/LocalLow/')
        if (!gameName && lowIdx >= 0) {
          const afterLow = rest.slice(lowIdx + 'AppData/LocalLow/'.length)
          const segs = afterLow.split('/')
          if (segs.length >= 2) gameName = `${segs[0]}/${segs[1]}`
          else if (segs.length === 1) gameName = segs[0]
        }
        if (!gameName) {
          const savedIdx = rest.indexOf('Saved Games')
          if (savedIdx >= 0) gameName = 'Saved Games'
        }
        const key = `prefix:${prefixId}:${gameName}`
        if (!groups.has(key)) {
          groups.set(key, { key, label: gameName || `Prefix ${prefixId}`, prefixId, files: [] })
        }
        groups.get(key)!.files.push(file)
      } else {
        let label = 'Other saves'
        const parts2 = file.relative_path.split('/')
        const steamIdx = parts2.indexOf('steam')
        if (steamIdx >= 0 && parts2[steamIdx + 1] === 'userdata') label = 'Steam userdata'
        const ludIdx = parts2.indexOf('ludusavi')
        if (ludIdx >= 0) label = 'Ludusavi'
        const unityIdx = parts2.indexOf('unity3d')
        if (unityIdx >= 0) label = 'Unity3D'
        const key = `other:${label}`
        if (!groups.has(key)) {
          groups.set(key, { key, label, files: [] })
        }
        groups.get(key)!.files.push(file)
      }
    }
    const result = [...groups.values()]
    result.sort((a, b) => a.label.localeCompare(b.label))
    return result
  }

  function groupSizeBytes(fileList: PreferenceFile[]): number {
    return fileList.reduce((acc, f) => acc + (f.size_bytes || 0), 0)
  }

  function requestRestore(group: SaveGroup) {
    if (!group.prefixId) return
    dispatch('restore', { deviceId: device.id, prefixId: group.prefixId, gameName: group.label })
  }

  function toggleGroup(key: string) {
    expandedGroups = { ...expandedGroups, [key]: !expandedGroups[key] }
  }

  async function loadNotes() {
    notesLoading = true
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/notes`), { headers: authHeaders })
      if (!response.ok) throw new Error(`${response.status}`)
      notes = await response.json()
      if (!Array.isArray(notes)) notes = []
    } catch {
      // keep previous
    } finally {
      notesLoading = false
    }
  }

  async function loadAttachments() {
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/attachments`), { headers: authHeaders })
      if (!response.ok) throw new Error(`${response.status}`)
      attachments = await response.json()
      if (!Array.isArray(attachments)) attachments = []
    } catch {
      // keep previous
    }
  }

  async function createNote() {
    if (!noteContent.trim() || noteSaving) return
    noteSaving = true
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/notes`), {
        method: 'POST',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: noteContent.trim() })
      })
      if (!response.ok) throw new Error(`${response.status}`)
      noteContent = ''
      await loadNotes()
    } catch (e) {
      notify(e instanceof Error ? e.message : 'Failed to save note', 'error')
    } finally {
      noteSaving = false
    }
  }

  async function updateNote(noteId: string) {
    if (!editNoteContent.trim()) return
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/notes/${noteId}`), {
        method: 'PUT',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ content: editNoteContent.trim() })
      })
      if (!response.ok) throw new Error(`${response.status}`)
      editingNoteId = null
      editNoteContent = ''
      await loadNotes()
    } catch (e) {
      notify(e instanceof Error ? e.message : 'Failed to update note', 'error')
    }
  }

  async function deleteNote(noteId: string) {
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/notes/${noteId}`), {
        method: 'DELETE',
        headers: authHeaders
      })
      if (!response.ok) throw new Error(`${response.status}`)
      await loadNotes()
    } catch (e) {
      notify(e instanceof Error ? e.message : 'Failed to delete note', 'error')
    }
  }

  async function uploadFile(event: Event) {
    const input = event.target as HTMLInputElement
    const file = input.files?.[0]
    if (!file) return
    if (file.size > 8 * 1024 * 1024) {
      notify('File too large (max 8 MB)', 'error')
      input.value = ''
      return
    }
    uploadUploading = true
    try {
      const formData = new FormData()
      formData.append('file', file)
      if (uploadCaption.trim()) {
        formData.append('caption', uploadCaption.trim())
      }
      if (uploadNoteContent.trim()) {
        formData.append('content', uploadNoteContent.trim())
      }
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/attachments`), {
        method: 'POST',
        headers: authHeaders,
        body: formData
      })
      if (!response.ok) {
        const body = await response.json().catch(() => ({ error: 'failed' }))
        throw new Error(body.error || `${response.status}`)
      }
      await loadAttachments()
      notify('File uploaded', 'success')
      uploadNoteContent = ''
      uploadCaption = ''
    } catch (e) {
      notify(e instanceof Error ? e.message : 'Upload failed', 'error')
    } finally {
      uploadUploading = false
      input.value = ''
    }
  }

  async function deleteAttachment(attId: string) {
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}/attachments/${attId}`), {
        method: 'DELETE',
        headers: authHeaders
      })
      if (!response.ok) throw new Error(`${response.status}`)
      await loadAttachments()
    } catch (e) {
      notify(e instanceof Error ? e.message : 'Failed to delete file', 'error')
    }
  }

  function extractGameName(path: string): string {
    const parts = path.split('/')
    const docsIdx = parts.indexOf('Documents')
    if (docsIdx >= 0 && docsIdx + 2 < parts.length) return parts[docsIdx + 1] || ''
    const roamingIdx = parts.indexOf('Roaming')
    if (roamingIdx >= 0 && roamingIdx + 1 < parts.length) return parts[roamingIdx + 1] || ''
    const steamIdx = parts.indexOf('steamapps')
    if (steamIdx >= 0 && steamIdx + 2 < parts.length) return parts[steamIdx + 1]?.replace('common/', '') || ''
    if (parts.includes('unity3d')) {
      const appIdx = parts.findIndex((p) => p === 'apps' || p === 'data')
      if (appIdx >= 0 && appIdx + 1 < parts.length) return parts[appIdx + 1] || ''
    }
    return ''
  }

  $: hw = device.hardware
  $: cpu = hw?.cpu_usage_percent || 0
  $: memPct = memoryPercent(hw)

  onMount(() => {
    activeTab = 'system'
    // Use preloaded files if available, otherwise start empty.
    files = initialFiles.length > 0 ? [...initialFiles] : []
    apps = []
    saves = []
    expandedGroups = {}
    selectedFile = null
    appQuery = ''
    if (initialFiles.length === 0) void loadFiles()
    clock = window.setInterval(() => (now = Date.now()), 15000)

    return () => {
      window.clearInterval(clock)
      window.clearTimeout(toastTimer)
    }
  })
</script>

{#if open || variant === 'page'}
  <div
    class={variant === 'page' ? 'device-page' : 'modal-backdrop'}
    role={variant === 'page' ? undefined : 'presentation'}
    on:click={variant === 'page' ? undefined : close}
    on:keydown={variant === 'page' ? undefined : handleBackdropKeydown}
  >
    <div
      class={variant === 'page' ? 'device-page-inner' : 'device-modal'}
      role={variant === 'page' ? undefined : 'dialog'}
      aria-modal={variant === 'page' ? undefined : 'true'}
      aria-labelledby="device-modal-title"
      tabindex="-1"
      on:click|stopPropagation
      on:keydown|stopPropagation
    >
      {#if toastMessage}
        <div class="toast {toastKind}" role="status">{toastMessage}</div>
      {/if}

      <div class="modal-header">
        <div>
          <p class="eyebrow">Device</p>
          <h2 id="device-modal-title">{device.hostname}</h2>
          <small>{device.user_id}</small>
        </div>
        <div class="header-right">
          <span class={`badge ${statusColor(computedStatus(device))}`} title={STATUS_HINTS[computedStatus(device)] || 'Unknown status'}>
            <i class="dot"></i>{computedStatus(device)}
          </span>
          {#if variant === 'modal'}
            <button class="secondary" on:click={close}>Close</button>
          {/if}
        </div>
      </div>

      <div class="device-id-row">
        <span class="device-id-label">Device ID</span>
        <code>{device.id}</code>
      </div>

      {#if device.last_error}
        <div class="health-alert" title={`${device.last_error}${device.last_error_at ? ` · ${formatTime(device.last_error_at)}` : ''}`}>
          <strong>Sync problem · {timeAgo(device.last_error_at, now)}</strong>
          <span>{device.last_error}</span>
        </div>
      {/if}

      <div class="tabs-wrap">
        <div class="tablist" role="tablist" aria-label={`${device.hostname} details`}>
          <button
            role="tab"
            id="tab-system"
            class:active={activeTab === 'system'}
            aria-selected={activeTab === 'system'}
            tabindex={activeTab === 'system' ? 0 : -1}
            on:click={() => setTab('system')}
            on:keydown={tabKeydown}
          >
            System
          </button>
          <button
            role="tab"
            id="tab-files"
            class:active={activeTab === 'files'}
            aria-selected={activeTab === 'files'}
            tabindex={activeTab === 'files' ? 0 : -1}
            on:click={() => setTab('files')}
            on:keydown={tabKeydown}
          >
            Files<span class="count">{files.length || device.preference_count || 0}</span>
          </button>
          <button
            role="tab"
            id="tab-apps"
            class:active={activeTab === 'apps'}
            aria-selected={activeTab === 'apps'}
            tabindex={activeTab === 'apps' ? 0 : -1}
            on:click={() => setTab('apps')}
            on:keydown={tabKeydown}
          >
            Packages<span class="count">{apps.length || device.app_count || 0}</span>
          </button>
          <button
            role="tab"
            id="tab-saves"
            class:active={activeTab === 'saves'}
            aria-selected={activeTab === 'saves'}
            tabindex={activeTab === 'saves' ? 0 : -1}
            on:click={() => setTab('saves')}
            on:keydown={tabKeydown}
          >
            Saves<span class="count">{saves.length || device.saves_count || 0}</span>
          </button>
          <button
            role="tab"
            id="tab-docker"
            class:active={activeTab === 'docker'}
            aria-selected={activeTab === 'docker'}
            tabindex={activeTab === 'docker' ? 0 : -1}
            on:click={() => setTab('docker')}
            on:keydown={tabKeydown}
          >
            Docker
          </button>
          <button
            role="tab"
            id="tab-security"
            class:active={activeTab === 'security'}
            aria-selected={activeTab === 'security'}
            tabindex={activeTab === 'security' ? 0 : -1}
            on:click={() => setTab('security')}
            on:keydown={tabKeydown}
          >
            Security
          </button>
          <button
            role="tab"
            id="tab-notes"
            class:active={activeTab === 'notes'}
            aria-selected={activeTab === 'notes'}
            tabindex={activeTab === 'notes' ? 0 : -1}
            on:click={() => setTab('notes')}
            on:keydown={tabKeydown}
          >
            Notes
          </button>
        </div>

        <div class="tab-panel-scroll">
        {#if activeTab === 'files'}
          <div class="panel" role="tabpanel" id="panel-files" aria-labelledby="tab-files">
            <div class="files-header">
              <strong>Synced preference files</strong>
              <span>{files.length}</span>
            </div>
            {#if filesLoading}
              <p class="muted">Loading files...</p>
            {:else if files.length === 0}
              <p class="muted">No preference files synced yet.</p>
            {:else}
              <ul class="file-list">
                {#each [...files].sort((a, b) => a.filename.localeCompare(b.filename)) as file (file.id)}
                  <li>
                    <div class="file-meta">
                      <span class="filename">{file.filename}</span>
                      <span class="chip" style={categoryChip(file.category)}>{file.category}</span>
                    </div>
                    <div class="file-actions">
                      <small>{formatBytes(file.size_bytes)}</small>
                      <small title={formatTime(file.synced_at)}>{timeAgo(file.synced_at, now)}</small>
                      <button class="inline" on:click={() => openFileModal(file)}>View</button>
                      <button class="inline" on:click={() => copyText(file.content)}>Copy</button>
                      <button class="inline danger" on:click={() => excludeFile(file)}>Exclude</button>
                    </div>
                  </li>
                {/each}
              </ul>
            {/if}
          </div>

        {:else if activeTab === 'apps'}
          <div class="panel" role="tabpanel" id="panel-apps" aria-labelledby="tab-apps">
            <div class="apps-toolbar">
              <div class="files-header"><strong>User-installed applications</strong><span>{apps.length}</span></div>
              <input class="app-search" bind:value={appQuery} placeholder="Search packages" aria-label="Search installed packages" />
            </div>
            {#if appsLoading && !apps.length}
              <p class="muted">Loading installed packages...</p>
            {:else if appsError && !apps.length}
              <p class="error-inline">{appsError} <button class="inline" on:click={loadApps}>Retry</button></p>
            {:else if apps.length}
              <div class="app-groups">
                {#each appSources as group (group.id)}
                  {@const groupApps = appsFor(group.id)}
                  {#if groupApps.length}
                    <section class="app-group">
                      <h3>{group.label}<span>{groupApps.length}</span></h3>
                      <ul class="app-list">
                        {#each visibleApps(group.id) as app (app.name)}
                          <li>
                            <div><strong>{app.name}</strong><small>{app.version || app.path || 'installed'}</small></div>
                            <button class="inline" on:click={() => reinstallApp(app)}>{app.source === 'appimage' ? 'Info' : 'Reinstall'}</button>
                          </li>
                        {/each}
                      </ul>
                      {#if groupApps.length > visibleApps(group.id).length}
                        <button class="inline show-more" on:click={() => (expandedGroups = { ...expandedGroups, [group.id]: true })}>
                          Show all {groupApps.length}
                        </button>
                      {/if}
                    </section>
                  {/if}
                {/each}
              </div>
            {:else}
              <p class="muted">The agent will report Flatpak, APT and pacman/AUR packages on its next inventory cycle.</p>
            {/if}
          </div>

        {:else if activeTab === 'saves'}
          <div class="panel" role="tabpanel" id="panel-saves" aria-labelledby="tab-saves">
            <div class="files-header">
              <strong>Game saves</strong>
              <span>{saves.length} files</span>
            </div>
            {#if savesLoading && !saves.length}
              <p class="muted">Loading save files...</p>
            {:else if savesError && !saves.length}
              <p class="error-inline">{savesError} <button class="inline" on:click={loadSaves}>Retry</button></p>
            {:else if saves.length === 0}
              <p class="muted">No game saves synced yet. The agent discovers saves from Hydra Launcher (Wine prefixes, Saved Games, AppData/Roaming), Steam userdata, and Unity3D config.</p>
            {:else}
              {@const groups = groupSavesByPrefix(saves)}
              <div class="saves-groups">
                {#each groups as group (group.key)}
                  {@const isExpanded = expandedGroups[group.key] !== false}
                  <div class="save-group">
                    <div class="save-group-header">
                      <button class="save-group-toggle" on:click={() => toggleGroup(group.key)} aria-expanded={isExpanded}>
                        <span class="save-group-chevron">{isExpanded ? '▾' : '▸'}</span>
                        <span class="save-group-name">{group.label}</span>
                        {#if group.prefixId}
                          <span class="save-group-prefix">prefix {group.prefixId}</span>
                        {/if}
                        <span class="save-group-meta">{group.files.length} files · {formatBytes(groupSizeBytes(group.files))}</span>
                      </button>
                    </div>
                    {#if group.prefixId}
                      <div class="save-group-actions">
                        <button class="inline" on:click={() => requestRestore(group)}>Restore…</button>
                      </div>
                    {/if}
                    {#if isExpanded}
                      <ul class="file-list save-group-files">
                        {#each group.files.sort((a, b) => a.filename.localeCompare(b.filename)) as file (file.id)}
                          <li>
                            <div class="file-meta">
                              <span class="filename">{file.filename}</span>
                              {#if file.encoding === 'base64'}
                                <span class="chip binary-chip">binary</span>
                              {/if}
                            </div>
                            <div class="file-actions">
                              <small>{formatBytes(file.size_bytes)}</small>
                              <small title={formatTime(file.synced_at)}>{timeAgo(file.synced_at, now)}</small>
                              <button class="inline danger" on:click={() => excludeFile(file)}>Exclude</button>
                            </div>
                          </li>
                        {/each}
                      </ul>
                    {/if}
                  </div>
                {/each}
              </div>
            {/if}
          </div>

        {:else if activeTab === 'logs'}
          <div class="panel" role="tabpanel" id="panel-logs" aria-labelledby="tab-logs">
            <DeviceLogs deviceId={device.id} {authHeaders} />
          </div>

        {:else if activeTab === 'docker'}
          <div class="panel" role="tabpanel" id="panel-docker" aria-labelledby="tab-docker">
            <DockerTab
              deviceId={device.id}
              {authHeaders}
            />
          </div>

        {:else if activeTab === 'security'}
          <div class="panel" role="tabpanel" id="panel-security" aria-labelledby="tab-security">
            <SecurityTab
              deviceId={device.id}
              {authHeaders}
              lynisAvailable={hw?.lynis_available ?? false}
              lynisInstallCmd={hw?.lynis_install_cmd ?? ''}
              agentStatus={device.status}
            />
          </div>

        {:else if activeTab === 'notes'}
          <div class="panel" role="tabpanel" id="panel-notes" aria-labelledby="tab-notes">
            <div class="notes-section">
              <div class="note-compose">
                <textarea
                  class="note-textarea"
                  bind:value={noteContent}
                  placeholder="Write a note about this device..."
                  rows="3"
                ></textarea>
                <div class="note-actions">
                  <button class="primary" on:click={createNote} disabled={!noteContent.trim() || noteSaving}>
                    {noteSaving ? 'Saving...' : 'Add note'}
                  </button>
                </div>
              </div>

              <div class="attachments-section">
                <div class="attachments-header">
                  <strong>Attachments</strong>
                  <label class="upload-btn">
                    <input type="file" accept="image/*,.txt,.log,.conf,.json,.xml,.yaml,.yml,.csv,.md" on:change={uploadFile} disabled={uploadUploading} />
                    {uploadUploading ? 'Uploading...' : '+ Upload file'}
                  </label>
                </div>
                {#if uploadNoteContent || uploadUploading}
                  <input class="caption-input" bind:value={uploadCaption} placeholder="Photo caption (optional)..." />
                  <textarea class="note-textarea" bind:value={uploadNoteContent} placeholder="Optional note about this file..." rows="2"></textarea>
                {:else}
                  <p class="muted">Images and text files up to 8 MB</p>
                {/if}
                {#if attachments.length > 0}
                  <div class="attachments-grid">
                    {#each attachments as att (att.id)}
                      <div class="attachment-card">
                        {#if att.mime_type.startsWith('image/')}
                          <a href={apiURL(`/api/devices/${device.id}/attachments/${att.id}${attAuth}`)} target="_blank" rel="noopener" class="attachment-preview">
                            <img src={apiURL(`/api/devices/${device.id}/attachments/${att.id}${attAuth}`)} alt={att.filename} loading="lazy" />
                          </a>
                        {:else}
                          <a href={apiURL(`/api/devices/${device.id}/attachments/${att.id}${attAuth}`)} target="_blank" rel="noopener" class="attachment-preview attachment-text">
                            <span class="file-icon">📄</span>
                          </a>
                        {/if}
                        <div class="attachment-info">
                          <a href={apiURL(`/api/devices/${device.id}/attachments/${att.id}${attAuth}`)} target="_blank" rel="noopener" class="attachment-name">{att.filename}</a>
                          {#if att.caption}
                            <span class="attachment-caption">{att.caption}</span>
                          {/if}
                          <div class="attachment-meta-row">
                            <span class="attachment-meta">{formatBytes(att.size_bytes)}</span>
                            <button class="attachment-delete" on:click={() => deleteAttachment(att.id)} title="Delete">✕</button>
                          </div>
                        </div>
                      </div>
                    {/each}
                  </div>
                {/if}
              </div>

              {#if notesLoading}
                <p class="muted">Loading notes...</p>
              {:else if notes.length === 0 && attachments.length === 0}
                <p class="muted">No notes or attachments yet.</p>
              {:else}
                <div class="notes-list">
                  {#each notes as note (note.id)}
                    <div class="note-card">
                      {#if editingNoteId === note.id}
                        <textarea class="note-textarea" bind:value={editNoteContent} rows="3"></textarea>
                        <div class="note-edit-bar">
                          <button class="note-edit-save" on:click={() => updateNote(note.id)}>Save</button>
                          <button class="note-edit-cancel" on:click={() => { editingNoteId = null; editNoteContent = '' }}>Cancel</button>
                        </div>
                      {:else}
                        <div class="note-header">
                          <span class="note-date">{new Date(note.updated_at).toLocaleString()}</span>
                          <div class="note-menu-wrapper">
                            <button class="note-menu-trigger" on:click={() => { openNoteMenuId = openNoteMenuId === note.id ? null : note.id }} title="More">⋯</button>
                            {#if openNoteMenuId === note.id}
                              <div class="note-menu">
                                <button class="note-menu-item" on:click={() => { editingNoteId = note.id; editNoteContent = note.content; openNoteMenuId = null }}>Edit</button>
                                <button class="note-menu-item danger" on:click={() => { deleteNote(note.id); openNoteMenuId = null }}>Delete</button>
                              </div>
                            {/if}
                          </div>
                        </div>
                        <pre class="note-content">{note.content}</pre>
                      {/if}
                    </div>
                  {/each}
                </div>
              {/if}
            </div>
          </div>

        {:else}
          <div class="panel" role="tabpanel" id="panel-system" aria-labelledby="tab-system">
            <SystemMetrics {device} {authHeaders} />
          </div>
        {/if}
        </div>
      </div>

      <dl class="meta-grid">
        <div><dt>Last seen</dt><dd title={formatTime(device.last_seen_at)}>{timeAgo(device.last_seen_at, now)}</dd></div>
        <div><dt>Last sync</dt><dd title={formatTime(device.last_sync_at)}>{timeAgo(device.last_sync_at, now)}</dd></div>
        {#if device.created_at}
          <div><dt>Registered</dt><dd title={formatTime(device.created_at)}>{timeAgo(device.created_at, now)}</dd></div>
        {/if}
      </dl>

      <!-- At-a-glance audit facts. An operator should be able to identify the
           machine, judge whether it is healthy and see what is at stake without
           visiting a single tab. -->
      <dl class="meta-grid identity">
        <div><dt>OS</dt><dd title={telemetry.operating_system || ''}>{telemetry.operating_system || '—'}</dd></div>
        <div><dt>Kernel</dt><dd title={telemetry.kernel_version || ''}>{telemetry.kernel_version || '—'}</dd></div>
        <div><dt>CPU</dt><dd class="truncate" title={telemetry.cpu_model || ''}>{telemetry.cpu_model || '—'}</dd></div>
        {#if telemetry.desktop_environment}
          <div><dt>Desktop</dt><dd>{telemetry.desktop_environment}</dd></div>
        {/if}
        <div><dt>Uptime</dt><dd>{formatDuration(telemetry.uptime_seconds || 0)}</dd></div>
        <div><dt>Agent</dt><dd>{telemetry.agent_version || '—'}</dd></div>
        <div><dt>Files</dt><dd>{files.length}</dd></div>
        <div><dt>Apps</dt><dd>{apps.length}</dd></div>
        <div><dt>Saves</dt><dd>{saves.length}</dd></div>
        <div><dt>Docker</dt><dd>{telemetry.docker_available ? (telemetry.docker_info ? `${telemetry.docker_info.running}/${telemetry.docker_info.total} running` : 'available') : 'no'}</dd></div>
        <div><dt>Lynis</dt><dd class:warn-value={telemetry.lynis_available === false}>{telemetry.lynis_available ? 'installed' : 'missing'}</dd></div>
        <div><dt>Reported</dt><dd title={formatTime(telemetry.collected_at)}>{telemetry.collected_at ? timeAgo(telemetry.collected_at, now) : '—'}</dd></div>
      </dl>

      {#if insights.length > 0}
        <div class="insight-strip" aria-label="Findings">
          <span class="insight-strip-label">Findings</span>
          {#each insights as insight, i (i)}
            <span class="insight-pill insight-{insight.type}">{insight.text}</span>
          {/each}
        </div>
      {/if}

      <footer class="danger-zone">
        <button class="inline danger" on:click={deleteDevice}>Remove device and data</button>
      </footer>
    </div>
  </div>
{/if}

{#if selectedFile}
  <div class="modal-backdrop file-backdrop" role="presentation" on:click={closeFileModal}>
    <div class="file-modal" role="dialog" aria-modal="true" aria-labelledby="file-modal-title" tabindex="-1" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-header">
        <div>
          <p class="eyebrow">{selectedFile.category}</p>
          <h2 id="file-modal-title">{selectedFile.filename}</h2>
          <small>{selectedFile.relative_path} · {formatBytes(selectedFile.size_bytes)} · synced {timeAgo(selectedFile.synced_at, now)}</small>
        </div>
        <button class="secondary" on:click={closeFileModal}>Close</button>
      </div>
      <pre class="file-content">{selectedFile.content}</pre>
      <div class="modal-actions">
        <button on:click={() => copyText(selectedFile?.content || '')}>Copy content</button>
      </div>
    </div>
  </div>
{/if}

<style>
  /* Page mode: same content, but it fills the shell's main area instead of
     floating over the dashboard. Split out from .device-modal so the modal
     variant keeps its fixed height while this one grows with the viewport. */
  .device-page {
    width: 100%;
    min-height: 100%;
  }

  .device-page-inner {
    display: flex;
    flex-direction: column;
    gap: 0.85rem;
    width: 100%;
    max-width: 1400px;
    margin: 0 auto;
  }

  .device-modal {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 1.2rem;
    width: min(1280px, 96vw);
    height: min(860px, 92vh);
    /* The modal box is fixed and the panel scrolls inside it, so every tab has
       the same height and the tab bar never scrolls out of view. */
    overflow: hidden;
    border: 1px solid rgba(96, 165, 250, 0.3);
    border-radius: 14px;
    background: #0f172a;
    box-shadow: 0 24px 80px rgba(2, 6, 23, 0.5);
  }

  .modal-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 0.75rem;
  }

  .modal-header h2 {
    margin: 0.15rem 0;
    font-size: 1.15rem;
  }

  .modal-header small {
    color: #94a3b8;
  }

  .header-right {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-shrink: 0;
  }

  .device-id-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    padding: 0.4rem 0.6rem;
    border: 1px solid rgba(148, 163, 184, 0.12);
    border-radius: 7px;
    background: rgba(2, 6, 23, 0.35);
  }

  .device-id-label {
    color: #94a3b8;
    font-size: 0.6rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    white-space: nowrap;
  }

  .device-id-row code {
    color: #cbd5e1;
    font-family: ui-monospace, SFMono-Regular, monospace;
    font-size: 0.68rem;
    word-break: break-all;
    overflow-wrap: break-word;
  }

  .health-alert {
    display: grid;
    gap: 0.12rem;
    padding: 0.5rem 0.6rem;
    border-left: 3px solid #f87171;
    border-radius: 0 8px 8px 0;
    background: rgba(127, 29, 29, 0.2);
    color: #fecaca;
    font-size: 0.74rem;
  }

  .health-alert strong {
    font-size: 0.68rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
  }

  .health-alert span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tabs-wrap {
    display: flex;
    flex: 1 1 auto;
    flex-direction: column;
    /* min-height:0 is required so the panel can shrink and scroll instead of
       pushing the tab bar out of the fixed-height modal. */
    min-height: 0;
    min-width: 0;
  }

  .tab-panel-scroll {
    flex: 1 1 auto;
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
    padding-right: 0.3rem;
  }

  .tablist {
    display: flex;
    flex-wrap: wrap;
    gap: 0.2rem;
    border-bottom: 1px solid rgba(148, 163, 184, 0.16);
  }

  .tablist button {
    background: transparent;
    border: 0;
    border-bottom: 2px solid transparent;
    border-radius: 7px 7px 0 0;
    padding: 0.5rem 0.85rem;
    color: #94a3b8;
    font-size: 0.8rem;
    font-weight: 600;
    white-space: nowrap;
    display: inline-flex;
    align-items: center;
    /* Without this the flex children shrink below their content width and the
       labels render clipped instead of the strip scrolling. */
    flex: 0 0 auto;
  }

  .tablist button:hover {
    color: #e2e8f0;
  }

  .tablist button.active {
    color: #ccfbf1;
    border-bottom-color: #2dd4bf;
    background: rgba(13, 148, 136, 0.12);
  }

  .tablist .count {
    margin-left: 0.35rem;
    min-width: 1.15rem;
    height: 1.15rem;
    padding: 0 0.35rem;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border-radius: 999px;
    background: rgba(59, 130, 246, 0.16);
    color: #93c5fd;
    font-size: 0.64rem;
  }

  .meta-grid.identity dt {
    color: #64748b;
  }

  .meta-grid dd.truncate {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .meta-grid dd.warn-value {
    color: #fbbf24;
  }

  .insight-strip {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 0.3rem;
    padding: 0.5rem 0.6rem;
    border: 1px solid rgba(148, 163, 184, 0.14);
    border-radius: 8px;
    background: rgba(2, 6, 23, 0.35);
  }

  .insight-strip-label {
    color: #64748b;
    font-size: 0.62rem;
    font-weight: 600;
    letter-spacing: 0.07em;
    text-transform: uppercase;
  }

  .insight-pill {
    padding: 0.1rem 0.45rem;
    border: 1px solid transparent;
    border-radius: 999px;
    font-size: 0.68rem;
  }

  .insight-pill.info {
    color: #93c5fd;
    border-color: rgba(96, 165, 250, 0.35);
    background: rgba(59, 130, 246, 0.12);
  }

  .insight-pill.warn {
    color: #fcd34d;
    border-color: rgba(251, 191, 36, 0.35);
    background: rgba(251, 191, 36, 0.12);
  }

  .insight-pill.crit {
    color: #fca5a5;
    border-color: rgba(248, 113, 113, 0.4);
    background: rgba(248, 113, 113, 0.14);
  }

  .panel {
    padding-top: 0.65rem;
    display: grid;
    gap: 0.6rem;
    min-width: 0;
  }

  .files-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 0.5rem;
    color: #e2e8f0;
    font-size: 0.8rem;
  }

  .files-header span {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 1.4rem;
    height: 1.4rem;
    padding: 0 0.35rem;
    border-radius: 999px;
    background: rgba(59, 130, 246, 0.15);
    color: #93c5fd;
    font-size: 0.72rem;
  }

  /* Each list scrolls on its own so a long inventory does not push the rest of
     the panel around, and so the tab content height stays stable. */
  .file-list,
  .app-list,
  .saves-groups,
  .notes-list,
  .file-list,
  .app-list,
  .saves-groups,
  .notes-list {
    list-style: none;
    padding: 0;
    margin: 0;
    display: grid;
    align-content: start;
    gap: 0.4rem;
    max-height: min(52vh, 480px);
    overflow-y: auto;
    overscroll-behavior: contain;
    padding-right: 0.35rem;
  }

  .panel-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.6rem;
    flex-wrap: wrap;
  }

  /* The log list styles moved to screens/DeviceLogs.svelte, which owns the
     whole log viewer now that both the tab and the sidebar section use it. */

  .file-list li {
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.35rem 0.6rem;
    padding: 0.45rem 0.55rem;
    background: rgba(15, 23, 42, 0.45);
    border: 1px solid rgba(148, 163, 184, 0.12);
    border-radius: 9px;
  }

  .file-meta {
    display: grid;
    gap: 0.18rem;
    min-width: 0;
  }

  .filename {
    color: #f8fafc;
    font-weight: 600;
    font-size: 0.82rem;
    word-break: break-word;
  }

  .chip {
    justify-self: start;
    width: fit-content;
    padding: 0.05rem 0.45rem;
    border: 1px solid rgba(148, 163, 184, 0.3);
    border-radius: 999px;
    font-size: 0.6rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
  }

  .file-actions {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.3rem;
    color: #cbd5e1;
  }

  .file-actions small {
    color: #94a3b8;
    font-size: 0.64rem;
  }

  .apps-toolbar {
    display: grid;
    gap: 0.45rem;
  }

  .app-search {
    padding: 0.5rem 0.6rem;
    border-radius: 7px;
    font-size: 0.8rem;
  }

  .app-groups {
    display: grid;
    gap: 0.5rem;
    max-height: 28rem;
    overflow: auto;
  }

  .app-group {
    overflow: hidden;
    border: 1px solid rgba(148, 163, 184, 0.15);
    border-radius: 8px;
    background: rgba(49, 46, 129, 0.16);
  }

  .app-group h3 {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin: 0;
    padding: 0.45rem 0.6rem;
    background: rgba(30, 41, 59, 0.55);
    color: #c4b5fd;
    font-size: 0.7rem;
    letter-spacing: 0.07em;
    text-transform: uppercase;
  }

  .app-group h3 span {
    color: #94a3b8;
    font-weight: 400;
  }

  .app-list {
    list-style: none;
    padding: 0 0.6rem;
    margin: 0;
  }

  .app-list li {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 0.4rem;
    min-height: 2.1rem;
    padding: 0.3rem 0;
    border-bottom: 1px solid rgba(148, 163, 184, 0.1);
  }

  .app-list li:last-child {
    border-bottom: 0;
  }

  .app-list li > div {
    display: grid;
    min-width: 0;
    gap: 0.05rem;
  }

  .app-list li strong {
    display: block;
    max-width: 13rem;
    overflow: hidden;
    font-size: 0.78rem;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .app-list small {
    max-width: 13rem;
    overflow: hidden;
    color: #94a3b8;
    font-size: 0.64rem;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .show-more {
    margin: 0.35rem 0.75rem 0.6rem;
  }

  .meta-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(130px, 1fr));
    gap: 0.45rem;
    margin: 0;
    padding: 0.6rem;
    border: 1px solid rgba(148, 163, 184, 0.14);
    border-radius: 9px;
    background: rgba(2, 6, 23, 0.35);
  }

  .meta-grid div {
    display: grid;
    gap: 0.1rem;
    min-width: 0;
  }

  .meta-grid dt {
    font-size: 0.62rem;
    color: #94a3b8;
    text-transform: uppercase;
    letter-spacing: 0.07em;
  }

  .meta-grid dd {
    margin: 0;
    color: #f8fafc;
    font-size: 0.8rem;
    word-break: break-word;
  }

  .danger-zone {
    display: flex;
    justify-content: flex-end;
    margin-top: 0.5rem;
    padding-top: 0.55rem;
    border-top: 1px dashed rgba(148, 163, 184, 0.14);
  }

  .muted {
    margin: 0;
    color: #94a3b8;
    font-size: 0.78rem;
  }

  .error-inline {
    margin: 0;
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 0.5rem;
    color: #fda4af;
    font-size: 0.8rem;
  }

  .toast {
    position: sticky;
    top: 0;
    z-index: 10;
    margin-bottom: 0.5rem;
    padding: 0.6rem 0.85rem;
    border: 1px solid rgba(45, 212, 191, 0.4);
    border-left-width: 3px;
    border-radius: 10px;
    background: #0f2f35;
    color: #ccfbf1;
    font-size: 0.8rem;
  }

  .toast.error {
    border-color: rgba(248, 113, 113, 0.45);
    background: #3b1219;
    color: #fecaca;
  }

  .saves-groups {
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }

  .save-group {
    border: 1px solid rgba(148, 163, 184, 0.15);
    border-radius: 10px;
    overflow: hidden;
  }

  .save-group-header {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    width: 100%;
    padding: 0;
    background: rgba(15, 23, 42, 0.6);
    border: none;
    border-radius: 10px;
  }

  .save-group-toggle {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex: 1;
    padding: 0.6rem 0.8rem;
    background: transparent;
    border: none;
    border-radius: 10px;
    color: #e5eefb;
    font-size: 0.85rem;
    cursor: pointer;
    text-align: left;
  }

  .save-group-toggle:hover {
    background: rgba(15, 23, 42, 0.4);
  }

  .save-group-chevron {
    color: #64748b;
    font-size: 0.75rem;
    flex-shrink: 0;
  }

  .save-group-name {
    font-weight: 600;
    color: #e2e8f0;
  }

  .save-group-prefix {
    font-size: 0.7rem;
    color: #64748b;
    background: rgba(100, 116, 139, 0.15);
    padding: 0.1rem 0.4rem;
    border-radius: 4px;
  }

  .save-group-meta {
    margin-left: auto;
    font-size: 0.75rem;
    color: #94a3b8;
  }

  .save-group-files {
    border-top: 1px solid rgba(148, 163, 184, 0.1);
    margin: 0;
  }

  .save-group-files li {
    padding-left: 1.5rem;
  }

  .binary-chip {
    background: rgba(245, 158, 11, 0.15) !important;
    border-color: rgba(245, 158, 11, 0.3) !important;
    color: #fcd34d !important;
  }

  .file-content {
    min-height: 14rem;
    max-height: 58vh;
    overflow: auto;
    margin: 0;
    padding: 0.9rem;
    border: 1px solid rgba(148, 163, 184, 0.15);
    border-radius: 9px;
    background: #020617;
    color: #dbeafe;
    font: 0.8rem/1.6 ui-monospace, SFMono-Regular, monospace;
    white-space: pre;
  }

  .modal-actions {
    display: flex;
    justify-content: flex-end;
  }

  .file-backdrop {
    z-index: 40;
  }

  .file-modal {
    display: flex;
    flex-direction: column;
    gap: 0.9rem;
    padding: 1.2rem;
    width: min(900px, 95vw);
    max-height: min(760px, 92vh);
    border: 1px solid rgba(96, 165, 250, 0.3);
    border-radius: 14px;
    background: #0f172a;
    box-shadow: 0 24px 80px rgba(2, 6, 23, 0.5);
  }

  .notes-section { display: flex; flex-direction: column; gap: 1rem; }

  .note-compose { display: flex; flex-direction: column; gap: 0.5rem; }

  .note-textarea {
    width: 100%;
    min-height: 60px;
    padding: 0.6rem;
    background: #020617;
    border: 1px solid rgba(148, 163, 184, 0.2);
    border-radius: 8px;
    color: #e2e8f0;
    font-size: 0.85rem;
    font-family: inherit;
    resize: vertical;
  }
  .note-textarea:focus { outline: none; border-color: #3b82f6; }

  .note-edit-bar {
    display: flex;
    gap: 0.5rem;
    justify-content: flex-end;
  }

  .note-edit-save {
    border: none;
    background: none;
    color: #3b82f6;
    font-size: 0.78rem;
    cursor: pointer;
    padding: 0;
  }
  .note-edit-save:hover { color: #60a5fa; }

  .note-edit-cancel {
    border: none;
    background: none;
    color: #64748b;
    font-size: 0.78rem;
    cursor: pointer;
    padding: 0;
  }
  .note-edit-cancel:hover { color: #94a3b8; }

  .notes-list { display: flex; flex-direction: column; gap: 0.5rem; }

  .note-card {
    background: #020617;
    border: 1px solid rgba(148, 163, 184, 0.12);
    border-radius: 8px;
    padding: 0.7rem;
  }

  .note-content {
    margin: 0;
    font-size: 0.82rem;
    color: #cbd5e1;
    white-space: pre-wrap;
    word-break: break-word;
    font-family: inherit;
  }

  .note-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 0.35rem;
  }

  .note-date { font-size: 0.7rem; color: #64748b; }

  .note-menu-wrapper { position: relative; }

  .note-menu-trigger {
    border: none;
    background: none;
    color: #64748b;
    font-size: 1rem;
    cursor: pointer;
    padding: 0.1rem 0.35rem;
    border-radius: 4px;
    line-height: 1;
    transition: background 0.15s, color 0.15s;
  }
  .note-menu-trigger:hover {
    background: rgba(148, 163, 184, 0.12);
    color: #e2e8f0;
  }

  .note-menu {
    position: absolute;
    top: 100%;
    right: 0;
    background: #0f172a;
    border: 1px solid rgba(148, 163, 184, 0.18);
    border-radius: 6px;
    padding: 0.25rem 0;
    min-width: 100px;
    z-index: 20;
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.5);
  }

  .note-menu-item {
    display: block;
    width: 100%;
    text-align: left;
    border: none;
    background: none;
    color: #cbd5e1;
    font-size: 0.78rem;
    padding: 0.35rem 0.65rem;
    cursor: pointer;
    transition: background 0.12s;
  }
  .note-menu-item:hover { background: rgba(148, 163, 184, 0.1); }
  .note-menu-item.danger { color: #ef4444; }
  .note-menu-item.danger:hover { background: rgba(239, 68, 68, 0.1); }

  .attachments-section { display: flex; flex-direction: column; gap: 0.4rem; }

  .attachments-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .attachments-header strong { font-size: 0.85rem; color: #e2e8f0; }

  .upload-btn {
    font-size: 0.78rem;
    padding: 0.3rem 0.7rem;
    background: #1e293b;
    border: 1px solid rgba(148, 163, 184, 0.2);
    border-radius: 6px;
    color: #94a3b8;
    cursor: pointer;
    transition: background 0.15s;
  }
  .upload-btn:hover { background: #334155; color: #e2e8f0; }
  .upload-btn input { display: none; }

  .attachments-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(120px, 1fr));
    gap: 0.5rem;
    margin-top: 0.3rem;
  }

  .attachment-card {
    position: relative;
    background: #020617;
    border: 1px solid rgba(148, 163, 184, 0.12);
    border-radius: 8px;
    overflow: hidden;
  }

  .attachment-preview {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 90px;
    background: #0f172a;
    overflow: hidden;
  }
  .attachment-preview img {
    width: 100%;
    height: 100%;
    object-fit: cover;
  }
  .attachment-text {
    font-size: 2rem;
  }

  .attachment-info {
    padding: 0.3rem 0.4rem;
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
  }

  .attachment-name {
    font-size: 0.7rem;
    color: #93c5fd;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    text-decoration: none;
  }
  .attachment-name:hover { text-decoration: underline; }

  .attachment-caption {
    font-size: 0.65rem;
    color: #94a3b8;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .caption-input {
    width: 100%;
    padding: 0.4rem 0.6rem;
    background: #020617;
    border: 1px solid rgba(148, 163, 184, 0.2);
    border-radius: 6px;
    color: #e2e8f0;
    font-size: 0.8rem;
    font-family: inherit;
  }
  .caption-input:focus { outline: none; border-color: #3b82f6; }

  .attachment-meta { font-size: 0.6rem; color: #64748b; }

  .attachment-meta-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    width: 100%;
  }

  .attachment-delete {
    border: none;
    background: none;
    color: #64748b;
    font-size: 0.65rem;
    cursor: pointer;
    padding: 0;
    opacity: 0;
    transition: color 0.15s;
  }
  .attachment-card:hover .attachment-delete { opacity: 1; }
  .attachment-delete:hover { color: #ef4444; }
</style>
