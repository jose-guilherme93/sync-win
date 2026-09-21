<script lang="ts">
  import { onMount } from 'svelte'
  import DeviceModal from './components/DeviceModal.svelte'
  import SimpleMetrics from './components/SimpleMetrics.svelte'
  import Sparkline from './components/Sparkline.svelte'
  import NotificationsModal from './components/NotificationsModal.svelte'
  import NotificationToast from './components/NotificationToast.svelte'
  import { addTelemetryPoint } from './lib/telemetry-store'
  import { serverBase } from './lib/api'

  type Device = {
    id: string
    hostname: string
    user_id: string
    status: string
    last_seen_at: string
    last_sync_at: string
    created_at?: string
    hardware?: HardwareStats
    apps?: AppInfo[]
    app_count: number
    preference_count: number
    saves_count: number
    saves_size_bytes: number
    saves_last_synced_at?: string
    last_error?: string
    last_error_at?: string
  }

  type AppInfo = { name: string; version: string; source: string; path?: string }

  type HardwareStats = {
    cpu_usage_percent: number
    memory_used_bytes: number
    memory_total_bytes: number
    disk_read_bytes: number
    disk_write_bytes: number
    disk_read_rate: number
    disk_write_rate: number
    uptime_seconds: number
    load_average: string
    cpu_model: string
    kernel_version: string
    operating_system: string
    power_watts: number
    architecture?: string
    desktop_environment?: string
    locale?: string
    timezone?: string
    agent_version?: string
    collected_at: string
    cpu_core_usage?: number[]
    swap_used_bytes?: number
    swap_total_bytes?: number
    memory_buffers_bytes?: number
    memory_cached_bytes?: number
    disk_partitions?: { mount: string; device: string; total_bytes: number; used_bytes: number; free_bytes: number; used_percent: number }[]
    network_ifaces?: { name: string; rx_bytes: number; tx_bytes: number; rx_rate?: number; tx_rate?: number; rx_packets: number; tx_packets: number; rx_errors: number; tx_errors: number }[]
    cpu_temperature?: number
    gpu_temperature_celsius?: number
    battery_percent?: number
    battery_status?: string
    top_cpu_processes?: { pid: number; name: string; cpu_percent: number; mem_rss_bytes: number }[]
    top_mem_processes?: { pid: number; name: string; cpu_percent: number; mem_rss_bytes: number }[]
    agent_cpu_usage?: number
    agent_memory_bytes?: number
    docker_available?: boolean
    docker_containers?: any[]
    docker_info?: any
  }

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

  type StatusFilter = 'all' | 'online' | 'degraded' | 'errors'
  type LiveState = 'live' | 'paused' | 'reconnecting'

  // The address embedded in the install command. It defaults to how the
  // dashboard is being accessed right now, but stays editable so the user can
  // point agents at the server's LAN address when the dashboard was opened
  // locally on the Docker host.
  const localAccess = /^(localhost|127\.0\.0\.1|::1|\[::1\])$/.test(window.location.hostname)
  let installServer = serverBase
  const POLL_MS = 10000
  const MAX_POLL_MS = 60000
  const PANEL_REFRESH_MS = 60000

  const TAB_NAMES = ['files', 'apps', 'saves', 'system'] as const
  type TabName = (typeof TAB_NAMES)[number]

  const STATUS_HINTS: Record<string, string> = {
    online: 'Agent reported within the last 30 seconds',
    stale: 'No report for over 30 seconds',
    offline: 'No report for more than 5 minutes',
    error: 'The agent reported a sync failure'
  }

  let devices: Device[] = []
  let filesByDevice: Record<string, PreferenceFile[]> = {}
  let appsByDevice: Record<string, AppInfo[]> = {}
  let savesByDevice: Record<string, PreferenceFile[]> = {}
  let appsLoading: Record<string, boolean> = {}
  let savesLoading: Record<string, boolean> = {}
  let appsError: Record<string, string> = {}
  let savesError: Record<string, string> = {}
  let loading = true
  let devicesLoading = false
  let error = ''
  let now = Date.now()
  let liveState: LiveState = 'live'
  let lastUpdated = 0
  let consecutiveFailures = 0
  let pollTimer = 0
  let toastTimer = 0
  let lastDevicesJson = ''
  let lastCacheWrite = 0
  let lastPanelLoad = 0

  let anonymousId = ''
  let ownerIdentity = ''
  let authReady = false
  let authToken = ''
  let authVersion = 0 // prevents stale restoreSession from overwriting fresh login
  let email = ''
  let password = ''
  let authMode: 'login' | 'register' = 'login'
  let authError = ''
  let sessionExpired = false
  let accountEmail = ''

  $: signedIn = Boolean(authToken && ownerIdentity && accountEmail)

  let selectedFile: PreferenceFile | null = null
  let selectedDeviceId = ''
  let activeTab: Record<string, TabName> = {}
  let installOpen = localStorage.getItem('lem-install-open') === '1'
  let deviceQuery = ''
  let statusFilter: StatusFilter = 'all'
  let appQuery = ''

  type MetricsMode = 'simple' | 'complex'
  let metricsMode: MetricsMode = (localStorage.getItem('lem-metrics-mode') as MetricsMode) || 'simple'
  let deviceModalOpen = false
  let deviceDetails: Record<string, Device> = {}
  // The modal prefers the full detail payload (logs, processes, Docker state)
  // and falls back to the lightweight list entry while it loads.
  $: modalDevice = selectedDeviceId
    ? deviceDetails[selectedDeviceId] || devices.find((d) => d.id === selectedDeviceId) || null
    : null

  let toastMessage = ''
  let toastKind: 'success' | 'error' = 'success'
  let lastFocused: HTMLElement | null = null
  let settingsOpen = false
  let notificationsOpen = false
  let notifEvents: { id: number; type: string; device_id: string; hostname: string; message: string; created_at: string }[] = []
  let lastNotifId = 0
  let notifSound = 'beep'
  let extraDirs: string[] = []
  let newExtraDir = ''
  let settingsLoading = false
  let restoringState: Record<string, boolean> = {}
  let restoreModalOpen = false
  let restoreTargetDevice: Device | null = null
  let restoreSourceDevice: Device | null = null
  let restorePrefixId = ''
  let restoreGameName = ''

  let userMenuOpen = false
  let bellOpen = false
  let logsOpen = false
  let logsTab: 'all' | 'audit' | 'errors' = 'all'
  let logsEntries: any[] = []
  let logsLoading = false
  let logsOffset = 0
  let logsTotal = 0
  let logsFilter = ''
  let addDeviceOpen = false
  let profileEmail = ''
  let profileCurrentPassword = ''
  let profileNewPassword = ''
  let profileSaving = false
  let profileError = ''
  let retentionOpen = false
  let retentionRawHours = 2
  let retention1mDays = 7
  let retention5mDays = 30
  let retention1hDays = 365
  let retentionSaving = false
  let retentionError = ''

  const authHeaders = (token: string): Record<string, string> => (token ? { Authorization: `Bearer ${token}` } : {})
  $: ownerHeaders = authHeaders(authToken)
  $: normalizedInstallServer = (() => {
    let s = installServer.trim().replace(/\/+$/, '') || serverBase
    if (s && !/^https?:\/\//.test(s)) s = `http://${s}`
    return s
  })()
  $: installServerIsLocal = /^(https?:\/\/)?(localhost|127\.0\.0\.1|::1|\[::1\])(:\d+)?(\/|$)/.test(normalizedInstallServer)
  let enrollmentToken = ''
  let enrollmentLoading = false
  let enrollmentError = ''
  $: installCommand = signedIn && enrollmentToken
    ? `curl -fsSL "${normalizedInstallServer}/install/${enrollmentToken}" -o /tmp/lem-install.sh && sudo bash /tmp/lem-install.sh`
    : ''
  $: if (installOpen && signedIn && !enrollmentToken && !enrollmentLoading) {
    requestEnrollmentToken()
  }

  $: kpis = devices.reduce(
    (acc, d) => {
      acc.total += 1
      const status = computedStatus(d)
      if (status === 'online') acc.online += 1
      if (status === 'offline' || status === 'stale') acc.degraded += 1
      if (status === 'error' || d.last_error) acc.errors += 1
      acc.files += d.preference_count || 0
      return acc
    },
    { total: 0, online: 0, degraded: 0, errors: 0, files: 0 }
  )

  $: query = deviceQuery.trim().toLowerCase()
  $: filteredDevices = [...devices]
    .filter((d) => {
      const matchQuery = !query || `${d.hostname} ${d.user_id} ${d.id}`.toLowerCase().includes(query)
      const status = computedStatus(d)
      const matchStatus =
        statusFilter === 'all' ||
        (statusFilter === 'online' && status === 'online') ||
        (statusFilter === 'degraded' && (status === 'offline' || status === 'stale')) ||
        (statusFilter === 'errors' && (status === 'error' || !!d.last_error))
      return matchQuery && matchStatus
    })
    .sort((a, b) => severity(a) - severity(b) || a.hostname.localeCompare(b.hostname))

  function severity(d: Device) {
    const status = computedStatus(d)
    if (status === 'error' || d.last_error) return 0
    if (status === 'offline') return 1
    if (status === 'stale') return 2
    if (status === 'online') return 4
    return 3
  }

  function setStatusFilter(next: StatusFilter) {
    statusFilter = statusFilter === next ? 'all' : next
  }

  // The dashboard is account-first: every device belongs to the signed-in
  // account. On load we trust cached credentials immediately and show
  // cached data while validating the session in the background.
  async function restoreSession() {
    const storedToken = localStorage.getItem('lem-auth-token') || ''
    const storedOwner = localStorage.getItem('lem-owner-id') || ''
    const storedEmail = localStorage.getItem('lem-account-email') || ''
    const myVersion = ++authVersion

    if (storedToken && storedOwner) {
      // Optimistically trust cached credentials — show dashboard instantly
      authToken = storedToken
      ownerIdentity = storedOwner
      accountEmail = storedEmail
      authReady = true

      // Load cached devices immediately (sync, no network)
      loadCachedDevices()

      // Connect notification SSE with cached credentials
      connectNotifSSE()

      // Validate session + refresh devices in parallel (non-blocking)
      fetch(`${serverBase}/api/auth/me`, {
        headers: { Authorization: `Bearer ${storedToken}` }
      }).then(async (response) => {
        // Skip if a newer auth flow (login) has started
        if (myVersion !== authVersion) return
        if (response.ok) {
          const payload = await response.json()
          ownerIdentity = payload.owner_id
          accountEmail = payload.email
          localStorage.setItem('lem-owner-id', ownerIdentity)
          localStorage.setItem('lem-account-email', accountEmail)
        } else if (response.status === 401) {
          clearAccountStorage()
          authToken = ''
          ownerIdentity = ''
          accountEmail = ''
          devices = []
          lastDevicesJson = ''
          sessionExpired = true
        }
      }).catch(() => {
        // Network error — keep using cached credentials
      })

      // Refresh devices from server (non-blocking)
      loadDevices(false)
    } else {
      // No cached credentials — show login form
      authReady = true
    }
  }

  let notifSSE: EventSource | null = null

  function connectNotifSSE() {
    if (notifSSE) return
    // EventSource cannot set Authorization headers, so the owner is passed as
    // a query parameter — the server accepts owner_id as a fallback.
    const owner = encodeURIComponent(ownerIdentity || 'anonymous')
    notifSSE = new EventSource(`${serverBase}/api/notifications/stream?owner_id=${owner}`)
    notifSSE.onmessage = (ev) => {
      try {
        const event = JSON.parse(ev.data)
        if (event && event.id && !notifEvents.some(e => e.id === event.id)) {
          notifEvents = [...notifEvents, event]
        }
      } catch {}
    }
    notifSSE.onerror = () => {
      notifSSE?.close()
      notifSSE = null
      // Reconnect after 5 seconds unless the component is being torn down.
      setTimeout(() => {
        if (!notifSSE) connectNotifSSE()
      }, 5000)
    }
  }

  function disconnectNotifSSE() {
    if (notifSSE) {
      notifSSE.close()
      notifSSE = null
    }
  }

  function loadCachedDevices() {
    const raw = localStorage.getItem(`lem-devices-${ownerIdentity}`)
    if (raw) {
      try {
        const cached = JSON.parse(raw)
        if (Array.isArray(cached) && cached.length > 0) {
          devices = cached
          lastDevicesJson = raw
          loading = false
        }
      } catch {}
    }
  }

  function clearAccountStorage() {
    localStorage.removeItem('lem-auth-token')
    localStorage.removeItem('lem-owner-id')
    localStorage.removeItem('lem-account-email')
    localStorage.removeItem(`lem-devices-${ownerIdentity}`)
  }

  async function loadDevices(showLoading = false) {
    if (devicesLoading) return
    devicesLoading = true
    try {
      if (showLoading && devices.length === 0) loading = true
      const response = await fetch(`${serverBase}/api/devices`, { headers: ownerHeaders })
      if (!response.ok) throw new Error(`request failed: ${response.status}`)
      const payload = await response.json()
      if (!Array.isArray(payload)) throw new Error('invalid devices response')
      const serialized = JSON.stringify(payload)
      if (serialized !== lastDevicesJson) {
        lastDevicesJson = serialized
        devices = payload
        for (const dev of payload) {
          if (dev?.id && dev.hardware) {
            addTelemetryPoint(dev.id, dev.hardware)
          }
        }
        localStorage.setItem(`lem-devices-${ownerIdentity}`, serialized)
        lastCacheWrite = Date.now()
        if (selectedDeviceId && Date.now() - lastPanelLoad > PANEL_REFRESH_MS) {
          selectPanels(selectedDeviceId)
          ensureApps(selectedDeviceId)
        }
      }
      lastUpdated = Date.now()
      consecutiveFailures = 0
      liveState = 'live'
      error = ''
    } catch (err) {
      consecutiveFailures += 1
      liveState = 'reconnecting'
      if (!lastUpdated) error = err instanceof Error ? err.message : 'unknown error'
    } finally {
      devicesLoading = false
      loading = false
    }
  }

  async function pollTick() {
    if (document.hidden) {
      liveState = 'paused'
      schedulePoll(POLL_MS)
      return
    }
    await Promise.all([loadDevices(false), pollNotifications()])
    if (deviceModalOpen && selectedDeviceId) {
      void loadDeviceDetail(selectedDeviceId)
    }
    schedulePoll(Math.min(POLL_MS * 2 ** Math.min(consecutiveFailures, 4), MAX_POLL_MS))
  }

  function schedulePoll(delay: number) {
    window.clearTimeout(pollTimer)
    pollTimer = window.setTimeout(() => void pollTick(), delay)
  }

  function selectPanels(deviceId: string) {
    void loadFiles(deviceId)
    lastPanelLoad = Date.now()
    if (getTab(deviceId) === 'apps') ensureApps(deviceId)
    if (getTab(deviceId) === 'saves') ensureSaves(deviceId)
  }

  // App inventories are only fetched when the Packages tab is actually
  // opened, and reused for a minute instead of refetching on every expand.
  const PANEL_TTL_MS = 60000
  let appsLoadedAt: Record<string, number> = {}

  function ensureApps(deviceId: string) {
    const loadedAt = appsLoadedAt[deviceId] || 0
    if (!appsByDevice[deviceId]?.length || Date.now() - loadedAt > PANEL_TTL_MS) {
      void loadApps(deviceId)
    }
  }

  async function loadFiles(deviceId: string) {
    try {
      const response = await fetch(`${serverBase}/api/devices/${deviceId}`, { headers: ownerHeaders })
      if (!response.ok) throw new Error(`file request failed: ${response.status}`)
      const payload = await response.json()
      if (!Array.isArray(payload)) throw new Error('invalid files response')
      filesByDevice = { ...filesByDevice, [deviceId]: payload }
      // Cache to localStorage for instant access on next visit
      localStorage.setItem(`lem-files-${deviceId}`, JSON.stringify(payload))
    } catch (err) {
      // Fallback to cache on error
      const cached = localStorage.getItem(`lem-files-${deviceId}`)
      if (cached) {
        try { filesByDevice = { ...filesByDevice, [deviceId]: JSON.parse(cached) } } catch { /* ignore */ }
      } else {
        filesByDevice = { ...filesByDevice, [deviceId]: [] }
      }
      notify('Could not load synced files', 'error')
      console.error(err)
    }
  }

  async function loadApps(deviceId: string) {
    appsLoading = { ...appsLoading, [deviceId]: true }
    appsError = { ...appsError, [deviceId]: '' }
    try {
      const response = await fetch(`${serverBase}/api/devices/${deviceId}/apps`, { headers: ownerHeaders })
      if (!response.ok) throw new Error(`app request failed: ${response.status}`)
      const payload = await response.json()
      if (!Array.isArray(payload)) throw new Error('invalid app inventory response')
      appsByDevice = { ...appsByDevice, [deviceId]: payload }
      appsLoadedAt = { ...appsLoadedAt, [deviceId]: Date.now() }
      localStorage.setItem(`lem-apps-${deviceId}`, JSON.stringify(payload))
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Could not load app inventory'
      appsError = { ...appsError, [deviceId]: message }
      const cached = localStorage.getItem(`lem-apps-${deviceId}`)
      if (!appsByDevice[deviceId] && cached) {
        try { appsByDevice = { ...appsByDevice, [deviceId]: JSON.parse(cached) } } catch { /* ignore invalid cache */ }
      }
    } finally {
      appsLoading = { ...appsLoading, [deviceId]: false }
    }
  }

  async function loadSaves(deviceId: string) {
    savesLoading = { ...savesLoading, [deviceId]: true }
    savesError = { ...savesError, [deviceId]: '' }
    try {
      // Use cached device files if available, otherwise fetch
      let files = filesByDevice[deviceId]
      if (!files) {
        const response = await fetch(`${serverBase}/api/devices/${deviceId}`, { headers: ownerHeaders })
        if (!response.ok) throw new Error(`saves request failed: ${response.status}`)
        files = await response.json()
        if (!Array.isArray(files)) throw new Error('invalid device files response')
        filesByDevice = { ...filesByDevice, [deviceId]: files }
      }
      savesByDevice = { ...savesByDevice, [deviceId]: files.filter((f: PreferenceFile) => f.category === 'saves') }
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Could not load save files'
      savesError = { ...savesError, [deviceId]: message }
    } finally {
      savesLoading = { ...savesLoading, [deviceId]: false }
    }
  }

  function ensureSaves(deviceId: string) {
    if (!savesByDevice[deviceId]?.length) {
      void loadSaves(deviceId)
    }
  }

  async function loadSyncConfig() {
    settingsLoading = true
    try {
      const response = await fetch(`${serverBase}/api/sync-config`, { headers: ownerHeaders })
      if (!response.ok) throw new Error(`config request failed: ${response.status}`)
      const payload = await response.json()
      extraDirs = Array.isArray(payload.extra_dirs) ? payload.extra_dirs : []
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not load settings', 'error')
    } finally {
      settingsLoading = false
    }
  }

  async function saveSyncConfig() {
    settingsLoading = true
    try {
      const response = await fetch(`${serverBase}/api/sync-config`, {
        method: 'PUT',
        headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ extra_dirs: extraDirs })
      })
      if (!response.ok) throw new Error(`config save failed: ${response.status}`)
      notify('Extra save folders updated.')
      settingsOpen = false
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not save settings', 'error')
    } finally {
      settingsLoading = false
    }
  }

  function addExtraDir() {
    const dir = newExtraDir.trim()
    if (!dir || extraDirs.includes(dir)) return
    extraDirs = [...extraDirs, dir]
    newExtraDir = ''
  }

  function removeExtraDir(dir: string) {
    extraDirs = extraDirs.filter((d) => d !== dir)
  }

  function openSettings() {
    settingsOpen = true
    void loadSyncConfig()
  }

  function closeSettings() {
    settingsOpen = false
  }

  function openNotifications() {
    notificationsOpen = true
    userMenuOpen = false
    bellOpen = false
  }

  function toggleBell() {
    bellOpen = !bellOpen
    if (bellOpen) userMenuOpen = false
  }

  function closeNotifications() {
    notificationsOpen = false
  }

  async function pollNotifications() {
    if (!signedIn) return
    try {
      const res = await fetch(`${serverBase}/api/notifications/inbox?since=${lastNotifId}`, { headers: ownerHeaders })
      if (!res.ok) return
      const events = await res.json()
      if (Array.isArray(events) && events.length > 0) {
        for (const e of events) {
          if (e.id > lastNotifId) lastNotifId = e.id
          if (!e.read) notifEvents = [...notifEvents, e]
        }
      }
    } catch {}
  }

  function dismissNotif(id: number) {
    notifEvents = notifEvents.filter((e) => e.id !== id)
    void fetch(`${serverBase}/api/notifications/inbox/read`, {
      method: 'POST',
      headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids: [id] }),
    })
  }

  function clearNotifs() {
    const ids = notifEvents.map((e) => e.id)
    notifEvents = []
    if (ids.length > 0) {
      void fetch(`${serverBase}/api/notifications/inbox/read`, {
        method: 'POST',
        headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids }),
      })
    }
  }

  function loadNotifSound() {
    notifSound = localStorage.getItem('lem-notif-sound') || 'beep'
  }

  function saveNotifSound(s: string) {
    notifSound = s
    localStorage.setItem('lem-notif-sound', s)
  }

  function selectDevice(deviceId: string) {
    if (!deviceId) return
    selectedDeviceId = deviceId
    deviceModalOpen = true
    void loadDeviceDetail(deviceId)
    selectPanels(deviceId)
  }

  async function loadDeviceDetail(deviceId: string) {
    if (!deviceId) return
    try {
      const response = await fetch(`${serverBase}/api/devices/${deviceId}/detail`, { headers: ownerHeaders })
      if (!response.ok) return
      const payload = await response.json()
      if (!payload || typeof payload !== 'object' || Array.isArray(payload) || !payload.id) return
      deviceDetails = { ...deviceDetails, [deviceId]: payload }
    } catch {
      // Keep the last known detail; the list summary still renders.
    }
  }

  function closeDeviceModal() {
    deviceModalOpen = false
    selectedDeviceId = ''
  }

  function setMetricsMode(mode: MetricsMode) {
    metricsMode = mode
    localStorage.setItem('lem-metrics-mode', mode)
  }

  function getTab(deviceId: string): TabName {
    return activeTab[deviceId] || 'files'
  }

  function setTab(deviceId: string, tab: TabName) {
    activeTab = { ...activeTab, [deviceId]: tab }
    if (tab === 'apps') ensureApps(deviceId)
    if (tab === 'saves') ensureSaves(deviceId)
  }

  function tabKeydown(event: KeyboardEvent, deviceId: string) {
    if (event.key !== 'ArrowLeft' && event.key !== 'ArrowRight') return
    event.preventDefault()
    const index = TAB_NAMES.indexOf(getTab(deviceId))
    const next = event.key === 'ArrowRight' ? (index + 1) % TAB_NAMES.length : (index - 1 + TAB_NAMES.length) % TAB_NAMES.length
    setTab(deviceId, TAB_NAMES[next])
    document.getElementById(`tab-${deviceId}-${TAB_NAMES[next]}`)?.focus()
  }

  function toggleInstall() {
    installOpen = !installOpen
    localStorage.setItem('lem-install-open', installOpen ? '1' : '0')
    if (installOpen && !enrollmentToken && signedIn) {
      requestEnrollmentToken()
    }
  }

  async function requestEnrollmentToken() {
    if (!signedIn || enrollmentLoading) return
    enrollmentLoading = true
    enrollmentError = ''
    try {
      const res = await fetch(`${serverBase}/api/agent/enroll-token`, {
        method: 'POST',
        headers: ownerHeaders,
      })
      if (!res.ok) {
        const body = await res.json().catch(() => ({ error: 'failed' }))
        throw new Error(body.error || `HTTP ${res.status}`)
      }
      const data = await res.json()
      enrollmentToken = data.token
    } catch (e: any) {
      enrollmentError = e.message || 'Failed to generate token'
    } finally {
      enrollmentLoading = false
    }
  }

  function refreshInstallToken() {
    enrollmentToken = ''
    requestEnrollmentToken()
  }

  async function restoreGameSaves(deviceId: string, prefixId: string, gameName: string) {
    restoreSourceDevice = devices.find((d) => d.id === deviceId) || null
    restorePrefixId = prefixId
    restoreGameName = gameName
    restoreTargetDevice = null
    restoreModalOpen = true
  }

  async function confirmRestore() {
    if (!restoreSourceDevice || !restoreTargetDevice || !restorePrefixId) return
    const key = `${restoreSourceDevice.id}:${restorePrefixId}:${restoreGameName}`
    restoringState = { ...restoringState, [key]: true }
    restoreModalOpen = false
    try {
      const response = await fetch(`${serverBase}/api/devices/${restoreSourceDevice.id}/restore-saves`, {
        method: 'POST',
        headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({
          prefix_id: restorePrefixId,
          game_name: restoreGameName,
          target_device_id: restoreTargetDevice.id
        })
      })
      const payload = await response.json().catch(() => ({}))
      if (!response.ok) throw new Error(payload.error || `request failed (${response.status})`)
      notify(`Restore queued: ${payload.files} files → ${restoreTargetDevice.hostname}. The agent will place them on the next cycle.`)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not queue restore', 'error')
    } finally {
      restoringState = { ...restoringState, [key]: false }
      restoreSourceDevice = null
      restoreTargetDevice = null
    }
  }

  function closeRestoreModal() {
    restoreModalOpen = false
    restoreSourceDevice = null
    restoreTargetDevice = null
  }

  const appSources = [
    { id: 'aur', label: 'AUR' },
    { id: 'pacman', label: 'Pacman' },
    { id: 'flatpak', label: 'Flatpak' },
    { id: 'apt', label: 'APT' },
    { id: 'appimage', label: 'AppImage' }
  ]

  const APPS_RENDER_LIMIT = 80
  let expandedGroups: Record<string, boolean> = {}

  function appsFor(deviceId: string, source: string) {
    const q = appQuery.trim().toLowerCase()
    return (appsByDevice[deviceId] || []).filter((app) => app.source === source && (!q || `${app.name} ${app.version}`.toLowerCase().includes(q)))
  }

  function visibleApps(deviceId: string, source: string) {
    const all = appsFor(deviceId, source)
    const key = `${deviceId}:${source}`
    if (appQuery.trim() || expandedGroups[key] || all.length <= APPS_RENDER_LIMIT) return all
    return all.slice(0, APPS_RENDER_LIMIT)
  }

  async function reinstallApp(deviceId: string, app: AppInfo) {
    if (app.source === 'appimage') {
      notify('AppImages are local files; reinstall is unavailable without a downloadable source.', 'error')
      return
    }
    try {
      const response = await fetch(`${serverBase}/api/devices/${deviceId}/apps?action=install`, {
        method: 'POST', headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ source: app.source, name: app.name })
      })
      if (!response.ok) throw new Error(`request failed: ${response.status}`)
      notify(`${app.name} queued for reinstall on the next agent cycle.`)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not queue reinstall', 'error')
    }
  }

  async function excludeFile(deviceId: string, file: PreferenceFile) {
    if (!window.confirm(`Exclude ${file.filename} from synchronization?`)) return
    try {
      const response = await fetch(`${serverBase}/api/devices/${deviceId}/files/${file.id}`, {
        method: 'DELETE', headers: ownerHeaders
      })
      if (!response.ok) throw new Error(`request failed: ${response.status}`)
      notify('File removed. The agent will apply the exclusion on its next cycle.')
      await loadFiles(deviceId)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not exclude file', 'error')
    }
  }

  async function deleteDevice(device: Device) {
    if (!window.confirm(`Remove ${device.hostname} and all synchronized data?`)) return
    try {
      const response = await fetch(`${serverBase}/api/devices/${device.id}`, { method: 'DELETE', headers: ownerHeaders })
      if (!response.ok) throw new Error(`request failed: ${response.status}`)
      devices = devices.filter((item) => item.id !== device.id)
      delete filesByDevice[device.id]
      if (selectedDeviceId === device.id) selectedDeviceId = ''
      notify('Device removed.')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not remove device', 'error')
    }
  }

  async function submitAuth() {
    try {
      authError = ''
      const response = await fetch(`${serverBase}/api/auth/${authMode}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password })
      })
      const payload = await response.json().catch(() => ({}))
      if (!response.ok) throw new Error(payload.error || `Request failed (${response.status})`)
      // Bump auth version so any in-flight restoreSession auth/me won't overwrite us
      authVersion++
      authToken = payload.token
      ownerIdentity = payload.owner_id
      accountEmail = payload.email
      sessionExpired = false
      localStorage.setItem('lem-auth-token', authToken)
      localStorage.setItem('lem-owner-id', ownerIdentity)
      localStorage.setItem('lem-account-email', accountEmail)
      devices = []
      lastDevicesJson = ''
      password = ''
      disconnectNotifSSE()
      connectNotifSSE()
      notify(`Signed in as ${accountEmail}`)
      await loadDevices(true)
    } catch (err) {
      authError = err instanceof Error ? err.message : 'Could not reach the server'
    }
  }

  function signOut() {
    clearAccountStorage()
    disconnectNotifSSE()
    authToken = ''
    accountEmail = ''
    ownerIdentity = ''
    devices = []
    filesByDevice = {}
    appsByDevice = {}
    selectedDeviceId = ''
    lastDevicesJson = ''
    error = ''
    loading = false
    notify('Signed out.')
  }

  function toggleUserMenu() {
    userMenuOpen = !userMenuOpen
    if (userMenuOpen) {
      profileEmail = accountEmail
      profileCurrentPassword = ''
      profileNewPassword = ''
      profileError = ''
    }
  }

  function closeUserMenu() {
    userMenuOpen = false
  }

  async function updateProfileEmail() {
    if (!profileEmail || profileEmail === accountEmail) return
    profileSaving = true
    profileError = ''
    try {
      const response = await fetch(`${serverBase}/api/auth/update-email`, {
        method: 'POST',
        headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: profileEmail })
      })
      const payload = await response.json().catch(() => ({}))
      if (!response.ok) throw new Error(payload.error || `Request failed (${response.status})`)
      accountEmail = payload.email
      localStorage.setItem('lem-account-email', accountEmail)
      profileEmail = accountEmail
      notify('Email updated.')
    } catch (err) {
      profileError = err instanceof Error ? err.message : 'Could not update email'
    } finally {
      profileSaving = false
    }
  }

  async function updateProfilePassword() {
    if (!profileCurrentPassword || !profileNewPassword) return
    profileSaving = true
    profileError = ''
    try {
      const response = await fetch(`${serverBase}/api/auth/update-password`, {
        method: 'POST',
        headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ current_password: profileCurrentPassword, new_password: profileNewPassword })
      })
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}))
        throw new Error(payload.error || `Request failed (${response.status})`)
      }
      profileCurrentPassword = ''
      profileNewPassword = ''
      notify('Password updated.')
    } catch (err) {
      profileError = err instanceof Error ? err.message : 'Could not update password'
    } finally {
      profileSaving = false
    }
  }

  function openLogs() {
    logsOpen = true
    logsTab = 'all'
    logsEntries = []
    logsOffset = 0
    logsTotal = 0
    logsFilter = ''
    userMenuOpen = false
    void loadLogs(true)
  }

  function closeLogs() {
    logsOpen = false
  }

  function openRetention() {
    userMenuOpen = false
    retentionOpen = true
    void loadRetention()
  }

  function closeRetention() {
    retentionOpen = false
  }

  async function loadRetention() {
    try {
      const response = await fetch(`${serverBase}/api/retention`, { headers: ownerHeaders })
      if (!response.ok) throw new Error(`${response.status}`)
      const data = await response.json()
      retentionRawHours = data.raw_hours ?? 2
      retention1mDays = data.resolution_1m_days ?? 7
      retention5mDays = data.resolution_5m_days ?? 30
      retention1hDays = data.resolution_1h_days ?? 365
      retentionError = ''
    } catch (e) {
      retentionError = e instanceof Error ? e.message : 'Failed to load retention'
    }
  }

  async function saveRetention() {
    retentionSaving = true
    retentionError = ''
    try {
      const response = await fetch(`${serverBase}/api/retention`, {
        method: 'PUT',
        headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({
          raw_hours: retentionRawHours,
          resolution_1m_days: retention1mDays,
          resolution_5m_days: retention5mDays,
          resolution_1h_days: retention1hDays
        })
      })
      if (!response.ok) throw new Error(`${response.status}`)
      retentionError = ''
    } catch (e) {
      retentionError = e instanceof Error ? e.message : 'Failed to save'
    } finally {
      retentionSaving = false
    }
  }

  async function loadLogs(reset = false) {
    if (logsLoading) return
    if (reset) {
      logsOffset = 0
      logsTotal = 0
      logsEntries = []
    }
    logsLoading = true
    try {
      const endpoint = logsTab === 'audit' ? '/api/audit' : logsTab === 'errors' ? '/api/errors' : '/api/logs'
      const params = new URLSearchParams({ limit: '50', offset: String(logsOffset) })
      if (logsFilter) params.set('search', logsFilter)
      const response = await fetch(`${serverBase}${endpoint}?${params}`, { headers: ownerHeaders })
      if (!response.ok) throw new Error(`request failed: ${response.status}`)
      const payload = await response.json()
      const entries = payload.entries || payload
      logsEntries = reset ? entries : [...logsEntries, ...entries]
      logsTotal = payload.total || logsEntries.length
      logsOffset = logsEntries.length
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not load logs', 'error')
    } finally {
      logsLoading = false
    }
  }

  function switchLogsTab(tab: 'all' | 'audit' | 'errors') {
    logsTab = tab
    void loadLogs(true)
  }

  function openAddDevice() {
    addDeviceOpen = true
    if (signedIn && !enrollmentToken) {
      requestEnrollmentToken()
    }
  }

  function closeAddDevice() {
    addDeviceOpen = false
  }

  function formatLogTime(ts: string) {
    if (!ts) return ''
    const d = new Date(ts)
    const date = d.toLocaleDateString('en-CA')
    const time = d.toLocaleTimeString('en-GB', { hour12: false })
    return `${date} ${time}`
  }

  function handleClickOutside(event: MouseEvent) {
    const target = event.target as HTMLElement
    if (userMenuOpen && !target.closest('.user-menu-wrapper')) {
      userMenuOpen = false
    }
    if (bellOpen && !target.closest('.bell-wrapper')) {
      bellOpen = false
    }
  }

  function openFile(file: PreferenceFile) {
    lastFocused = document.activeElement as HTMLElement | null
    selectedFile = file
  }

  function closeFile() {
    selectedFile = null
    lastFocused?.focus()
    lastFocused = null
  }

  function autofocus(node: HTMLElement) {
    node.focus()
  }

  function handleWindowKeydown(event: KeyboardEvent) {
    if (event.key !== 'Escape') return
    if (selectedFile) closeFile()
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

  function extractGameName(path: string): string {
    const parts = path.split('/')
    const docsIdx = parts.indexOf('Documents')
    if (docsIdx >= 0 && docsIdx + 2 < parts.length) {
      return parts[docsIdx + 1] || ''
    }
    const roamingIdx = parts.indexOf('Roaming')
    if (roamingIdx >= 0 && roamingIdx + 1 < parts.length) {
      return parts[roamingIdx + 1] || ''
    }
    const steamIdx = parts.indexOf('steamapps')
    if (steamIdx >= 0 && steamIdx + 2 < parts.length) {
      return parts[steamIdx + 1]?.replace('common/', '') || ''
    }
    if (parts.includes('unity3d')) {
      const appIdx = parts.findIndex((p) => p === 'apps' || p === 'data')
      if (appIdx >= 0 && appIdx + 1 < parts.length) {
        return parts[appIdx + 1] || ''
      }
    }
    return ''
  }

  type SaveGroup = {
    key: string
    label: string
    prefixId?: string
    files: PreferenceFile[]
  }

  function groupSavesByPrefix(files: PreferenceFile[]): SaveGroup[] {
    const groups = new Map<string, SaveGroup>()
    for (const file of files) {
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
        if (steamIdx >= 0 && parts2[steamIdx + 1] === 'userdata') { label = 'Steam userdata' }
        const ludIdx = parts2.indexOf('ludusavi')
        if (ludIdx >= 0) { label = 'Ludusavi' }
        const unityIdx = parts2.indexOf('unity3d')
        if (unityIdx >= 0) { label = 'Unity3D' }
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

  function groupSizeBytes(files: PreferenceFile[]): number {
    return files.reduce((acc, f) => acc + (f.size_bytes || 0), 0)
  }

  function toggleGroup(key: string) {
    expandedGroups = { ...expandedGroups, [key]: !expandedGroups[key] }
  }

  function statusColor(status: string) {
    if (status === 'online') return 'green'
    if (status === 'offline' || status === 'stale') return 'orange'
    if (status === 'error') return 'red'
    return 'gray'
  }

  // Client-side status fallback: compute status from last_seen_at
  // This ensures devices show as offline even if the server status is stale
  function computedStatus(device: { status: string; last_seen_at: string }): string {
    // Don't override error or duplicate states
    if (device.status === 'error' || device.status === 'duplicate') return device.status
    const age = (Date.now() - Date.parse(device.last_seen_at)) / 1000
    if (age > 300) return 'offline'  // 5 minutes
    if (age > 30) return 'stale'     // 30 seconds
    return 'online'
  }

  function timeAgo(value: string | number | null | undefined, nowTs: number) {
    if (!value) return 'never'
    const t = typeof value === 'number' ? value : Date.parse(value)
    if (!Number.isFinite(t)) return 'never'
    const diff = Math.max(0, Math.floor((nowTs - t) / 1000))
    if (diff < 10) return 'just now'
    if (diff < 60) return `${diff}s ago`
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
    if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`
    return `${Math.floor(diff / 86400)}d ago`
  }

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

  function activityLabel(device: Device) {
    const cpu = device.hardware?.cpu_usage_percent || 0
    return cpu < 5 ? 'Idle' : 'Active'
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

  function generateInsights(h?: HardwareStats) {
    if (!h) return []
    const insights: { text: string; type: 'info' | 'warn' | 'crit' }[] = []
    const cpu = h.cpu_usage_percent || 0
    const mem = memoryPercent(h)
    const agentCpu = h.agent_cpu_usage || 0

    if (cpu > 90) insights.push({ text: `CPU ${cpu.toFixed(0)}%`, type: 'crit' })
    else if (cpu > 70) insights.push({ text: `CPU ${cpu.toFixed(0)}%`, type: 'warn' })

    if (mem > 90) insights.push({ text: `RAM ${mem.toFixed(0)}%`, type: 'crit' })
    else if (mem > 75) insights.push({ text: `RAM ${mem.toFixed(0)}%`, type: 'warn' })

    if (h.cpu_temperature && h.cpu_temperature > 85) insights.push({ text: `Temp ${h.cpu_temperature.toFixed(0)}C`, type: 'crit' })
    else if (h.cpu_temperature && h.cpu_temperature > 70) insights.push({ text: `Temp ${h.cpu_temperature.toFixed(0)}C`, type: 'warn' })

    if (h.battery_percent != null && h.battery_percent > 0 && h.battery_percent < 15 && h.battery_status !== 'charging') {
      insights.push({ text: `Battery ${h.battery_percent.toFixed(0)}%`, type: 'crit' })
    }

    if (h.disk_partitions) {
      for (const p of h.disk_partitions) {
        if (p.used_percent > 90) insights.push({ text: `${p.mount} ${p.used_percent.toFixed(0)}% full`, type: 'crit' })
        else if (p.used_percent > 80) insights.push({ text: `${p.mount} ${p.used_percent.toFixed(0)}%`, type: 'warn' })
      }
    }

    if (h.network_ifaces) {
      for (const iface of h.network_ifaces) {
        if (iface.rx_errors > 0 || iface.tx_errors > 0) {
          insights.push({ text: `Net err ${iface.name}`, type: 'warn' })
        }
      }
    }

    if (agentCpu > 5) insights.push({ text: `Agent ${agentCpu.toFixed(1)}%`, type: 'warn' })

    if (h.kernel_version) {
      const parts = h.kernel_version.split('.')
      if (parts.length >= 2) {
        const major = parseInt(parts[0])
        const minor = parseInt(parts[1])
        if (major < 5 || (major === 5 && minor < 15)) {
          insights.push({ text: `Kernel ${h.kernel_version}`, type: 'warn' })
        }
      }
    }

    return insights
  }

  function categoryChip(category: string) {
    let hash = 0
    for (let i = 0; i < category.length; i++) hash = (hash * 31 + category.charCodeAt(i)) >>> 0
    const hue = hash % 360
    return `background: hsl(${hue}, 45%, 16%); border-color: hsl(${hue}, 55%, 32%); color: hsl(${hue}, 85%, 80%)`
  }

  onMount(() => {
    void restoreSession()
    loadNotifSound()
    schedulePoll(POLL_MS)
    const clock = window.setInterval(() => (now = Date.now()), 15000)
    const onVisibility = () => {
      if (!document.hidden) {
        liveState = liveState === 'paused' ? 'live' : liveState
        window.clearTimeout(pollTimer)
        void pollTick()
      }
    }
    document.addEventListener('visibilitychange', onVisibility)
    document.addEventListener('click', handleClickOutside)
    return () => {
      window.clearTimeout(pollTimer)
      window.clearTimeout(toastTimer)
      window.clearInterval(clock)
      document.removeEventListener('visibilitychange', onVisibility)
      document.removeEventListener('click', handleClickOutside)
      disconnectNotifSSE()
    }
  })
</script>

<svelte:head>
  <title>LEM Dashboard</title>
</svelte:head>

<svelte:window on:keydown={handleWindowKeydown} />

<div class="page">
  {#if !authReady}
    <div class="auth-gate">
      <div class="auth-card">
        <p class="eyebrow">Linux Environment Manager</p>
        <h1>Connecting…</h1>
      </div>
    </div>
  {:else if !signedIn}
    <div class="auth-gate">
      <div class="auth-card" role="dialog" aria-labelledby="auth-gate-title">
        <p class="eyebrow">Linux Environment Manager</p>
        <h1 id="auth-gate-title">{authMode === 'login' ? 'Sign in' : 'Create your account'}</h1>
        <p class="gate-hint">Every device and agent install is registered under your account. Sign in first — then copy the install command to your Linux machines.</p>
        {#if sessionExpired && authMode === 'login'}
          <p class="error-inline">Your session expired. Sign in again to see your devices.</p>
        {/if}
        {#if authError}
          <p class="error-inline">{authError}</p>
        {/if}
        <form class="auth-modal-fields" on:submit|preventDefault={submitAuth}>
          <label>Email<input bind:value={email} type="email" autocomplete="email" placeholder="you@example.com" required /></label>
          <label>Password<input bind:value={password} type="password" autocomplete={authMode === 'login' ? 'current-password' : 'new-password'} placeholder="At least 8 characters" minlength={8} required /></label>
          <button type="submit">{authMode === 'login' ? 'Sign in' : 'Create account'}</button>
          <button type="button" class="secondary" on:click={() => (authMode = authMode === 'login' ? 'register' : 'login')}>
            {authMode === 'login' ? 'New here? Create account' : 'Already have an account? Sign in'}
          </button>
        </form>
      </div>
    </div>
  {:else}
  <header class="header">
    <div class="header-left">
      <span class="logo">LEM</span>
      <span class="header-subtitle">Linux Environment Manager</span>
    </div>
    <div class="header-right">
      <div class="live-pill {liveState}" title={liveState === 'paused' ? 'Updates pause while this tab is hidden' : `Polling every ${POLL_MS / 1000}s`} role="status">
        <i class="dot"></i>
        <span>{liveState === 'live' ? 'Live' : liveState === 'paused' ? 'Paused' : 'Reconnecting'}</span>
        <small>{lastUpdated ? timeAgo(lastUpdated, now) : '—'}</small>
      </div>
      <button class="ghost" on:click={() => loadDevices(true)} disabled={devicesLoading}>Refresh</button>
      <div class="bell-wrapper">
        <button class="bell-btn" on:click|stopPropagation={toggleBell} title="Notifications">
          🔔
          {#if notifEvents.length > 0}<span class="bell-badge">{notifEvents.length}</span>{/if}
        </button>
        {#if bellOpen}
          <!-- svelte-ignore a11y-click-events-have-key-events -->
          <!-- svelte-ignore a11y-no-static-element-interactions -->
          <div class="bell-dropdown" on:click|stopPropagation>
            <div class="bell-header">
              <span class="bell-title">Notifications</span>
              {#if notifEvents.length > 0}
                <button class="bell-clear" on:click={clearNotifs}>Clear all</button>
              {/if}
            </div>
            <div class="bell-list">
              {#if notifEvents.length === 0}
                <div class="bell-empty">No new notifications</div>
              {:else}
                {#each notifEvents as e (e.id)}
                  <div class="bell-item bell-{e.type}">
                    <div class="bell-item-top">
                      <span class="bell-item-icon">{e.type === 'device_offline' ? '🔴' : e.type === 'device_online' ? '🟢' : e.type === 'sync_error' ? '⚠️' : '➕'}</span>
                      <span class="bell-item-host">{e.hostname || e.device_id}</span>
                      <span class="bell-item-time">{timeAgo(e.created_at, now)}</span>
                    </div>
                    <div class="bell-item-msg">{e.message}</div>
                    <button class="bell-item-dismiss" on:click={() => dismissNotif(e.id)}>✕</button>
                  </div>
                {/each}
              {/if}
            </div>
            {#if notifEvents.length > 0}
              <div class="bell-footer">
                <button class="bell-settings" on:click={openNotifications}>⚙ Notification settings</button>
              </div>
            {/if}
          </div>
        {/if}
      </div>
      <div class="user-menu-wrapper">
        <button class="avatar-btn" on:click|stopPropagation={toggleUserMenu} title={accountEmail}>
          {accountEmail.charAt(0).toUpperCase()}
        </button>
        {#if userMenuOpen}
          <!-- svelte-ignore a11y-click-events-have-key-events -->
          <!-- svelte-ignore a11y-no-static-element-interactions -->
          <div class="user-dropdown" on:click|stopPropagation>
            <div class="dropdown-header">
              <div class="avatar-large">{accountEmail.charAt(0).toUpperCase()}</div>
              <div>
                <div class="dropdown-email">{accountEmail}</div>
                <div class="dropdown-devices">{devices.length} device{devices.length !== 1 ? 's' : ''}</div>
              </div>
            </div>
            <div class="dropdown-divider"></div>
            <button class="dropdown-item" on:click={openSettings}>
              <span class="dropdown-icon">⚙</span> Settings
            </button>
            <button class="dropdown-item" on:click={openLogs}>
              <span class="dropdown-icon">📋</span> Logs
            </button>
            <button class="dropdown-item" on:click={openNotifications}>
              <span class="dropdown-icon">🔔</span> Notifications
            </button>
            <button class="dropdown-item" on:click={openRetention}>
              <span class="dropdown-icon">🗄</span> Data Retention
            </button>
            <div class="dropdown-divider"></div>
            <div class="dropdown-profile">
              <h4>Profile</h4>
              {#if profileError}<p class="error-inline">{profileError}</p>{/if}
              <form class="profile-form" on:submit|preventDefault={updateProfileEmail}>
                <label>Email
                  <input type="email" bind:value={profileEmail} placeholder={accountEmail} />
                </label>
                <button type="submit" disabled={profileSaving || profileEmail === accountEmail}>
                  {profileSaving ? 'Saving...' : 'Update email'}
                </button>
              </form>
              <form class="profile-form" on:submit|preventDefault={updateProfilePassword}>
                <label>Current password
                  <input type="password" bind:value={profileCurrentPassword} autocomplete="current-password" />
                </label>
                <label>New password
                  <input type="password" bind:value={profileNewPassword} minlength={8} autocomplete="new-password" />
                </label>
                <button type="submit" disabled={profileSaving || !profileNewPassword || !profileCurrentPassword}>
                  {profileSaving ? 'Saving...' : 'Change password'}
                </button>
              </form>
            </div>
            <div class="dropdown-divider"></div>
            <button class="dropdown-item danger" on:click={signOut}>
              Sign out
            </button>
          </div>
        {/if}
      </div>
    </div>
  </header>

  {#if toastMessage}
    <div class="toast {toastKind}" role="status">{toastMessage}</div>
  {/if}

  <section class="kpis" aria-label="Fleet summary">
    <button class="kpi" class:selected={statusFilter === 'all'} aria-pressed={statusFilter === 'all'} on:click={() => setStatusFilter('all')}>
      <strong>{kpis.total}</strong><span>Devices</span>
    </button>
    <button class="kpi k-online" class:selected={statusFilter === 'online'} aria-pressed={statusFilter === 'online'} on:click={() => setStatusFilter('online')}>
      <strong>{kpis.online}</strong><span>Online</span>
    </button>
    <button class="kpi k-degraded" class:selected={statusFilter === 'degraded'} aria-pressed={statusFilter === 'degraded'} on:click={() => setStatusFilter('degraded')}>
      <strong>{kpis.degraded}</strong><span>Degraded</span>
    </button>
    <button class="kpi k-errors" class:selected={statusFilter === 'errors'} aria-pressed={statusFilter === 'errors'} on:click={() => setStatusFilter('errors')}>
      <strong>{kpis.errors}</strong><span>Errors</span>
    </button>
    <div class="kpi static">
      <strong>{kpis.files}</strong><span>Synced files</span>
    </div>
  </section>

  <div class="toolbar">
    <input class="search-input" type="search" bind:value={deviceQuery} placeholder="Search by hostname, user or ID" aria-label="Search devices" />
    <button class="add-device-btn" on:click={openAddDevice}>+ Add Linux Device</button>
  </div>

  {#if loading}
    <div class="grid" aria-hidden="true">
      {#each [0, 1, 2] as i (i)}
        <article class="card skeleton">
          <div class="sk-line w40"></div>
          <div class="sk-line w70"></div>
          <div class="sk-row">
            <div class="sk-line w90"></div>
            <div class="sk-line w90"></div>
          </div>
          <div class="sk-line w70"></div>
        </article>
      {/each}
    </div>
  {:else if error}
    <div class="error-banner" role="alert">
      <span>{error}</span>
      <button class="inline" on:click={() => loadDevices(true)}>Try again</button>
    </div>
  {:else if devices.length === 0}
    <div class="empty">
      <strong>No devices yet</strong>
      <p>Run the install command above on a Linux machine. The device appears here within seconds — while it is still installing, before the agent even connects. The installer prints SUCCESS or troubleshooting steps when it finishes.</p>
      <p>Devices are tied to <em>this browser profile</em>{#if authToken} and your account{/if}. If you ran the command from another machine but opened this dashboard in a different browser (or incognito), the new device lives under that profile's identity instead.</p>
    </div>
  {:else if filteredDevices.length === 0}
    <div class="empty">
      <strong>No devices match</strong>
      <p>Adjust the search or clear the status filter.</p>
      <button class="inline" on:click={() => { deviceQuery = ''; statusFilter = 'all' }}>Clear filters</button>
    </div>
  {:else}
    <div class="device-list grid">
      {#each filteredDevices as device (device.id)}
        {@const cpu = device.hardware?.cpu_usage_percent || 0}
        {@const memPct = memoryPercent(device.hardware)}
        {@const insights = generateInsights(device.hardware)}
        {@const deviceStatus = computedStatus(device)}
        <!-- svelte-ignore a11y-click-events-have-key-events -->
        <!-- svelte-ignore a11y-no-static-element-interactions -->
        <article class="card clickable" on:click={() => selectDevice(device.id)}>
          <div class="card-left">
            <div class="card-top">
              <h2>{device.hostname}</h2>
              <span class={`badge ${statusColor(deviceStatus)}`} title={STATUS_HINTS[deviceStatus] || ''}>
                <i class="dot"></i>{deviceStatus}
              </span>
            </div>
            <p class="card-user">{device.user_id}</p>

            {#if metricsMode === 'complex'}
              <Sparkline {device} />
            {:else}
              <div class="device-summary">
                <div class="stat"><strong>{cpu.toFixed(0)}%</strong><span>CPU</span></div>
                <div class="stat"><strong>{memPct.toFixed(0)}%</strong><span>RAM</span></div>
                <div class="stat"><strong>{formatBytes(device.hardware?.memory_used_bytes || 0)}</strong><span>{formatBytes(device.hardware?.memory_total_bytes || 0)}</span></div>
                <div class="stat"><strong>{(device.hardware?.power_watts || 0).toFixed(1)}W</strong><span>power</span></div>
                <div class="stat"><strong>{formatDuration(device.hardware?.uptime_seconds || 0)}</strong><span>uptime</span></div>
                <div class="stat"><strong>{device.preference_count || 0}</strong><span>files</span></div>
                <div class="stat"><strong>{device.app_count || 0}</strong><span>pkgs</span></div>
                {#if (device.saves_count || 0) > 0}
                  <div class="stat saves-badge" title="Game saves"><strong>{device.saves_count}</strong><span>saves</span></div>
                {/if}
              </div>
            {/if}

            {#if device.last_error}
              <div class="health-alert" title={device.last_error}>
                <strong>Sync problem · {timeAgo(device.last_error_at, now)}</strong>
                <span>{device.last_error}</span>
              </div>
            {/if}
          </div>
          <div class="card-divider"></div>
          {#if insights.length > 0}
            <div class="card-insights">
              <span class="insights-label">Insights</span>
              {#each insights.slice(0, 6) as insight}
                <span class="insight-chip insight-{insight.type}">{insight.text}</span>
              {/each}
              {#if insights.length > 6}
                <span class="insight-more">+{insights.length - 6}</span>
              {/if}
            </div>
          {/if}
          <div class="card-open-hint">
            <span>Open details</span>
          </div>
        </article>
      {/each}
    </div>
    <div class="metrics-toggle-bar">
      <span class="metrics-label">Metrics view:</span>
      <div class="metrics-toggle">
        <button class:active={metricsMode === 'simple'} on:click={() => setMetricsMode('simple')}>Simple</button>
        <button class:active={metricsMode === 'complex'} on:click={() => setMetricsMode('complex')}>Complex</button>
      </div>
    </div>
  {/if}
  {/if}
</div>

{#if selectedFile}
  <div class="modal-backdrop" role="presentation" on:click={closeFile}>
    <div class="file-modal" role="dialog" aria-modal="true" aria-labelledby="file-modal-title" tabindex="-1" use:autofocus on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-header">
        <div>
          <p class="eyebrow">{selectedFile.category}</p>
          <h2 id="file-modal-title">{selectedFile.filename}</h2>
          <small>{selectedFile.relative_path} · {formatBytes(selectedFile.size_bytes)} · synced {timeAgo(selectedFile.synced_at, now)}</small>
        </div>
        <button class="secondary" on:click={closeFile}>Close</button>
      </div>
      <pre class="file-content">{selectedFile.content}</pre>
      <div class="modal-actions">
        <button on:click={() => copyText(selectedFile?.content || '')}>Copy content</button>
      </div>
    </div>
  </div>
{/if}

{#if settingsOpen}
  <div class="modal-backdrop" role="presentation" on:click={closeSettings}>
    <div class="settings-modal" role="dialog" aria-modal="true" aria-labelledby="settings-title" tabindex="-1" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-header">
        <div>
          <p class="eyebrow">Sync Configuration</p>
          <h2 id="settings-title">Save Folders</h2>
        </div>
        <button class="secondary" on:click={closeSettings}>Close</button>
      </div>

      <section class="settings-section">
        <h3>Built-in locations</h3>
        <p class="muted">The agent always scans these Hydra Launcher paths:</p>
        <ul class="settings-list">
          <li><code>~/.config/hydralauncher/wine-prefixes/*/drive_c/users/*/Documents</code></li>
          <li><code>~/.config/hydralauncher/wine-prefixes/*/drive_c/users/*/Saved Games</code></li>
          <li><code>~/.config/hydralauncher/wine-prefixes/*/drive_c/users/*/AppData/Roaming</code></li>
          <li><code>~/.config/hydralauncher/ludusavi/config.yaml</code></li>
          <li><code>~/.steam/steam/userdata</code></li>
          <li><code>~/.var/app/com.valvesoftware.Steam/.steam/steam/userdata</code></li>
          <li><code>~/.config/unity3d</code></li>
        </ul>
      </section>

      <section class="settings-section">
        <h3>Extra folders</h3>
        <p class="muted">Add additional directories to sync save files from. Each agent will scan these paths using the same extension filter and size limits.</p>
        {#if settingsLoading}
          <p class="muted">Loading...</p>
        {:else}
          <form class="extra-dirs-form" on:submit|preventDefault={addExtraDir}>
            <input type="text" bind:value={newExtraDir} placeholder="/path/to/saves or ~/more-saves" disabled={settingsLoading} />
            <button type="submit" disabled={settingsLoading || !newExtraDir.trim()}>Add</button>
          </form>
          {#if extraDirs.length === 0}
            <p class="muted">No extra folders configured.</p>
          {:else}
            <ul class="extra-dirs-list">
              {#each extraDirs as dir (dir)}
                <li>
                  <code>{dir}</code>
                  <button class="inline danger" on:click={() => removeExtraDir(dir)}>Remove</button>
                </li>
              {/each}
            </ul>
          {/if}
          <div class="settings-actions">
            <button on:click={saveSyncConfig} disabled={settingsLoading}>Save configuration</button>
          </div>
        {/if}
      </section>
    </div>
  </div>
{/if}

{#if restoreModalOpen && restoreSourceDevice}
  <div class="modal-backdrop" role="presentation" on:click={closeRestoreModal}>
    <div class="restore-modal" role="dialog" aria-modal="true" aria-labelledby="restore-title" tabindex="-1" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-header">
        <div>
          <p class="eyebrow">Restore saves</p>
          <h2 id="restore-title">Send to which device?</h2>
          <small>{restoreGameName || `prefix ${restorePrefixId}`} · from {restoreSourceDevice.hostname}</small>
        </div>
        <button class="secondary" on:click={closeRestoreModal}>Cancel</button>
      </div>
      <div class="restore-device-list">
        {#each devices.filter((d) => d.id !== restoreSourceDevice?.id) as target (target.id)}
          <button
            class="restore-device-option"
            class:selected={restoreTargetDevice?.id === target.id}
            on:click={() => (restoreTargetDevice = target)}
          >
            <span class="restore-device-name">{target.hostname}</span>
            <span class="restore-device-status {target.status}">
              <i class="dot"></i>{target.status}
            </span>
            <small>{target.user_id}</small>
          </button>
        {/each}
        {#if devices.filter((d) => d.id !== restoreSourceDevice?.id).length === 0}
          <p class="muted">No other devices available. Install the agent on another machine to restore saves there.</p>
        {/if}
      </div>
      <div class="restore-actions">
        <button on:click={confirmRestore} disabled={!restoreTargetDevice}>
          {restoreTargetDevice ? `Restore to ${restoreTargetDevice.hostname}` : 'Select a device'}
        </button>
      </div>
    </div>
  </div>
{/if}

{#if deviceModalOpen && modalDevice}
  <DeviceModal
    device={modalDevice}
    open={deviceModalOpen}
    on:close={closeDeviceModal}
    authHeaders={ownerHeaders}
    initialFiles={filesByDevice[modalDevice.id] || []}
  />
{/if}

{#if addDeviceOpen}
  <div class="modal-backdrop" role="presentation" on:click={closeAddDevice}>
    <div class="install-modal" role="dialog" aria-modal="true" aria-labelledby="install-modal-title" tabindex="-1" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-header">
        <div>
          <p class="eyebrow">Installation</p>
          <h2 id="install-modal-title">Add a Linux device</h2>
          <small>Run this once on the target machine. A temporary enrollment token is generated for each install.</small>
        </div>
        <button class="secondary" on:click={closeAddDevice}>Close</button>
      </div>
      {#if installServerIsLocal}
        <div class="warn-banner" role="alert">
          <strong>Dashboard accessed via localhost — this command only works on this machine.</strong>
          To install the agent on another network device, replace the address below with the Docker server's <strong>Tailscale IP</strong>
          (e.g. <code>http://100.x.x.x:8080</code>).
        </div>
      {/if}
      <label class="server-address-row">
        <span>Agent connects to</span>
        <input type="text" bind:value={installServer} spellcheck="false" aria-label="Server address used by installed agents" />
      </label>
      {#if enrollmentLoading}
        <div class="command-box">
          <pre>Generating enrollment token...</pre>
        </div>
      {:else if enrollmentError}
        <div class="command-box">
          <pre class="error">{enrollmentError}</pre>
          <button on:click={refreshInstallToken}>Retry</button>
        </div>
      {:else if installCommand}
        <div class="command-box">
          <pre>{installCommand}</pre>
          <div style="display:flex;gap:0.5rem;">
            <button on:click={() => copyText(installCommand)}>Copy install command</button>
            <button class="ghost" on:click={refreshInstallToken}>New token</button>
          </div>
        </div>
      {/if}
    </div>
  </div>
{/if}

{#if logsOpen}
  <div class="modal-backdrop" role="presentation" on:click={closeLogs}>
    <div class="logs-modal" role="dialog" aria-modal="true" aria-labelledby="logs-title" tabindex="-1" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-header">
        <div>
          <p class="eyebrow">Monitoring</p>
          <h2 id="logs-title">System Logs</h2>
        </div>
        <button class="secondary" on:click={closeLogs}>Close</button>
      </div>
      <div class="logs-tabs">
        <button class:active={logsTab === 'all'} on:click={() => switchLogsTab('all')}>All Logs</button>
        <button class:active={logsTab === 'audit'} on:click={() => switchLogsTab('audit')}>Audit Trail</button>
        <button class:active={logsTab === 'errors'} on:click={() => switchLogsTab('errors')}>Errors</button>
      </div>
      <div class="logs-filters">
        <input class="search-input" type="search" bind:value={logsFilter} placeholder="Search logs..." on:input={() => void loadLogs(true)} />
      </div>
      {#if logsLoading && logsEntries.length === 0}
        <div class="logs-loading">Loading...</div>
      {:else if logsEntries.length === 0}
        <div class="logs-empty">No logs found</div>
      {:else}
        <div class="logs-list">
          {#each logsEntries as entry}
            <div class="log-entry">
              <span class="log-ts">{formatLogTime(entry.timestamp)}</span>
              <span class="log-level level-{(entry.level || '').toLowerCase()}">{entry.level}</span>
              <span class="log-category">{entry.category}</span>
              <span class="log-event">{entry.event}</span>
              {#if entry.device_id}<span class="log-device">{entry.device_id}</span>{/if}
              {#if entry.message}<span class="log-message">{entry.message}</span>{/if}
              {#if entry.metadata && Object.keys(entry.metadata).length > 0}
                <span class="log-meta" title={JSON.stringify(entry.metadata, null, 2)}>{JSON.stringify(entry.metadata)}</span>
              {/if}
            </div>
          {/each}
        </div>
        {#if logsEntries.length < logsTotal}
          <div class="logs-pagination">
            <button class="ghost" on:click={() => void loadLogs(false)} disabled={logsLoading}>
              {logsLoading ? 'Loading...' : `Load more (${logsEntries.length} of ${logsTotal})`}
            </button>
          </div>
        {/if}
      {/if}
    </div>
  </div>
{/if}

{#if retentionOpen}
  <div class="modal-backdrop" role="presentation" on:click={closeRetention}>
    <div class="retention-modal" role="dialog" aria-modal="true" aria-labelledby="retention-title" tabindex="-1" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-header">
        <div>
          <p class="eyebrow">Storage</p>
          <h2 id="retention-title">Data Retention</h2>
        </div>
        <button class="secondary" on:click={closeRetention}>Close</button>
      </div>
      <div class="retention-body">
        <p class="retention-desc">Configure how long telemetry history is kept at each resolution. Older data is automatically deleted.</p>
        {#if retentionError}<p class="error-inline">{retentionError}</p>{/if}
        <div class="retention-grid">
          <label class="retention-field">
            <span>Raw (10s intervals)</span>
            <div class="retention-input">
              <input type="number" bind:value={retentionRawHours} min="1" max="168" />
              <span class="retention-unit">hours</span>
            </div>
          </label>
          <label class="retention-field">
            <span>1-minute aggregation</span>
            <div class="retention-input">
              <input type="number" bind:value={retention1mDays} min="1" max="365" />
              <span class="retention-unit">days</span>
            </div>
          </label>
          <label class="retention-field">
            <span>5-minute aggregation</span>
            <div class="retention-input">
              <input type="number" bind:value={retention5mDays} min="1" max="730" />
              <span class="retention-unit">days</span>
            </div>
          </label>
          <label class="retention-field">
            <span>1-hour aggregation</span>
            <div class="retention-input">
              <input type="number" bind:value={retention1hDays} min="1" max="3650" />
              <span class="retention-unit">days</span>
            </div>
          </label>
        </div>
        <button class="primary" on:click={saveRetention} disabled={retentionSaving}>
          {retentionSaving ? 'Saving...' : 'Save settings'}
        </button>
      </div>
    </div>
  </div>
{/if}

<NotificationsModal open={notificationsOpen} authHeaders={ownerHeaders} on:close={closeNotifications} />

{#if notifEvents.length > 0}
  <div class="notif-toast-stack">
    <div class="notif-toast-header">
      <strong>Alerts</strong>
      <button class="inline" on:click={clearNotifs}>Clear all</button>
    </div>
    {#each notifEvents.slice(-5) as evt (evt.id)}
      <NotificationToast event={evt} sound={notifSound} on:dismiss={() => dismissNotif(evt.id)} />
    {/each}
  </div>
{/if}
