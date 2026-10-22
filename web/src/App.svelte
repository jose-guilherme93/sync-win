<script lang="ts">
  import { onMount, onDestroy, tick } from 'svelte'
  import DeviceModal from './components/DeviceModal.svelte'
  import NotificationToast from './components/NotificationToast.svelte'
  import { addTelemetryPoint, resetTelemetry } from './lib/telemetry-store'
  import { apiFetch, apiURL, serverBase } from './lib/api'
  import { devToken, signInWithDevToken } from './lib/dev-auth'
  import { formatRelative, formatAbsolute } from './lib/format'
  import Sidebar from './components/shell/Sidebar.svelte'
  import Topbar from './components/shell/Topbar.svelte'
  import Home from './components/screens/Home.svelte'
  import DeviceOverview from './components/screens/DeviceOverview.svelte'
  import Alerts from './components/screens/Alerts.svelte'
  import Findings from './components/screens/Findings.svelte'
  import Reports from './components/screens/Reports.svelte'
  import Storage from './components/screens/Storage.svelte'
  import Network from './components/screens/Network.svelte'
  import Security from './components/screens/Security.svelte'
  import Docker from './components/screens/Docker.svelte'
  import CpuThermal from './components/screens/CpuThermal.svelte'
  import Memory from './components/screens/Memory.svelte'
  import Sensors from './components/screens/Sensors.svelte'
  import Processes from './components/screens/Processes.svelte'
  import Packages from './components/screens/Packages.svelte'
  import Services from './components/screens/Services.svelte'
  import DeviceLogs from './components/screens/DeviceLogs.svelte'
  import RemoteAccess from './components/screens/RemoteAccess.svelte'
  import DeviceSettings from './components/screens/DeviceSettings.svelte'
  import AppSettings from './components/screens/AppSettings.svelte'
  import Notifications from './components/screens/Notifications.svelte'
  import { nav, type Section } from './lib/router'
  import { selectedRange, DEFAULT_RANGE, type RangeId } from './lib/timerange'
  import { mergeDeviceDetail } from './lib/device'
  import type { AppInfo, Device, HardwareStats } from './lib/types'
  import type { SecurityPosture } from './lib/security-posture'
  import { uniquePorts } from './lib/ports'
  import { generateInsights } from './lib/insights'
  import { loadNotificationPrefs, notificationPrefs, resetNotificationPrefs, shouldToast } from './lib/notification-prefs'
  import { eventDetail, eventIcon } from './lib/notification-format'
  import { unlockNotificationAudio } from './lib/notification-sound'


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

  type LiveState = 'live' | 'paused' | 'reconnecting'

  // The address embedded in the install command. It defaults to how the
  // dashboard is being accessed right now, but stays editable so the user can
  // point agents at the server's LAN address when the dashboard was opened
  // locally on the Docker host.
  const localAccess = /^(localhost|127\.0\.0\.1|::1|\[::1\])$/.test(window.location.hostname)
  let installServer = serverBase
  // Install-time capabilities. SSH is genuinely opt-in (off); auto-update is
  // on by default because a device that stops updating is stranded, but the
  // operator can turn it off here.
  let enableSsh = false
  let enableAutoUpdate = true
  // The device list/heartbeat poll is fixed. It used to follow the chart range,
  // which meant a 30-day window polled every five minutes and made a healthy
  // device look stale. The topbar range now only controls how often the charts
  // refetch their history.
  let currentRange: RangeId = DEFAULT_RANGE
  const DEVICE_POLL_MS = 10000
  const MAX_POLL_MS = 60000
  const PANEL_REFRESH_MS = 60000
  // Top processes, Docker containers and the security posture only exist in
  // /detail. Refresh that snapshot on a slow cadence; the summary merge keeps
  // everything else live in between.
  const DETAIL_REFRESH_MS = 60000
  // The device list is polled every 10s but written to localStorage far less
  // often: it only speeds up the first paint, so a stale copy is harmless.
  const CACHE_WRITE_MS = 60000

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
  let lastDetailLoad = 0

  let anonymousId = ''
  let ownerIdentity = ''
  let authReady = false
  let authToken = ''
  let authVersion = 0 // prevents stale restoreSession from overwriting fresh login
  let email = ''
  let password = ''
  let authMode: 'login' | 'register' = 'login'
  let registrationEnabled = false
  let authError = ''
  let sessionExpired = false
  let accountEmail = ''

  $: signedIn = Boolean(ownerIdentity && accountEmail)

  let selectedFile: PreferenceFile | null = null
  let selectedDeviceId = ''
  let activeTab: Record<string, TabName> = {}
  let installOpen = localStorage.getItem('sync-win-install-open') === '1'
  let appQuery = ''

  let deviceDetails: Record<string, Device> = {}

  // Shell navigation. The sidebar writes into this store and the main area
  // reacts; nothing is rendered as a popup any more.
  let navState = { section: 'home' as Section, deviceId: null as string | null }
  const unsubNav = nav.subscribe((value) => {
    navState = value
    if (value.deviceId && value.deviceId !== selectedDeviceId) {
      selectedDeviceId = value.deviceId
      void loadDeviceDetail(value.deviceId)
      selectPanels(value.deviceId)
    }
    if (value.section === 'security' && value.deviceId && !(deviceDetails[value.deviceId] as any)?.securityPosture) {
      void loadDeviceDetail(value.deviceId, true)
    }
  })
  onDestroy(unsubNav)

  let sidebarCollapsed = localStorage.getItem('sync-win-sidebar-collapsed') === '1'
  let mobileNavOpen = false

  function toggleSidebar() {
    sidebarCollapsed = !sidebarCollapsed
    localStorage.setItem('sync-win-sidebar-collapsed', sidebarCollapsed ? '1' : '0')
  }

  function navigateTo(section: Section) {
    nav.setSection(section)
    mobileNavOpen = false
  }

  // Device-scoped sidebar sections map onto the tabs the device page already
  // renders, so the two navigations cannot disagree.
  $: deviceTabForSection = ({
    overview: 'system',
    cpu: 'system',
    memory: 'system',
    storage: 'system',
    network: 'system',
    sensors: 'system',
    containers: 'docker',
    packages: 'apps',
    security: 'security'
  } as Record<string, string>)[navState.section] || null

  $: isDeviceSection = Boolean(navState.deviceId) && navState.section !== 'home' && navState.section !== 'devices' && navState.section !== 'alerts' && navState.section !== 'findings' && navState.section !== 'reports' && navState.section !== 'configuration' && navState.section !== 'notifications'

  // Only the overview still uses the shared telemetry cards. Every other
  // device section now has a dedicated screen, so nothing else falls through.
  const HARDWARE_SECTIONS = new Set(['overview'])
  $: isHardwareSection = HARDWARE_SECTIONS.has(navState.section)
  // The selected device is the polled summary merged with the heavier detail
  // snapshot. Preferring the snapshot outright froze last_seen_at and hardware,
  // so the topbar turned stale while the list said online.
  $: modalDevice = selectedDeviceId
    ? mergeDeviceDetail(
        deviceDetails[selectedDeviceId],
        devices.find((d) => d.id === selectedDeviceId)
      )
    : null
  // The Security screen's posture is stored on the device-detail object so it
  // rides the same reactive path as the rest of the detail payload.
  $: activeSecurityPosture = ((modalDevice as any)?.securityPosture as SecurityPosture) || {
    publicPorts: [],
    pendingUpdates: 0,
    pendingStatus: '',
    failedServices: [],
    loaded: false,
    error: ''
  }

  let toastMessage = ''
  let toastKind: 'success' | 'error' = 'success'
  let lastFocused: HTMLElement | null = null
  let settingsOpen = false
  let notifEvents: { id: number; type: string; device_id: string; hostname: string; message: string; created_at: string }[] = []
  let lastNotifId = 0
  let notifPrefsLoaded = false
  // The toasts live in their own list. A toast is a decision taken once, when
  // the event arrives: filtering the inbox on every render would make a toast
  // appear and disappear as the event aged, and it would replay the whole
  // history every time the dashboard was opened.
  let toasts: { id: number; type: string; device_id: string; hostname: string; message: string; created_at: string }[] = []
  let extraDirs: string[] = []
  let newExtraDir = ''
  let workspaceDirs: string[] = []
  let newWorkspaceDir = ''
  let settingsLoading = false
  let restoringState: Record<string, boolean> = {}
  let restoreModalOpen = false
  let restoreTargetDevice: Device | null = null
  let restoreSourceDevice: Device | null = null
  let restorePrefixId = ''
  let restoreGameName = ''

  let userMenuOpen = false
  let bellOpen = false
  let addDeviceOpen = false
  let profileEmail = ''
  let profileCurrentPassword = ''
  let profileNewPassword = ''
  let profileSaving = false
  let profileError = ''
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
  let enrollmentAttempted = false
  let enrollmentError = ''
  $: installQuery = (() => {
    const params = new URLSearchParams()
    if (enableSsh) params.set('ssh', '1')
    if (!enableAutoUpdate) params.set('autoupdate', '0')
    return params.toString()
  })()
  $: installCommand = signedIn && enrollmentToken
    ? `curl -fsSL "${normalizedInstallServer}/install/${enrollmentToken}${installQuery ? `?${installQuery}` : ''}" -o /tmp/sync-win-install.sh && sudo bash /tmp/sync-win-install.sh`
    : ''
  // Fetch the enrollment token once per open. The `enrollmentAttempted` guard
  // is essential: without it a failed request leaves loading=false and token
  // empty, and this reactive statement would refire forever (request storm).
  $: if (installOpen && signedIn && !enrollmentToken && !enrollmentLoading && !enrollmentAttempted) {
    enrollmentAttempted = true
    requestEnrollmentToken()
  }

  // Public self-registration is disabled by default on the server. The auth
  // gate only offers "Create account" when the server explicitly allows it.
  async function loadAuthConfig() {
    try {
      const response = await apiFetch(apiURL('/api/auth/config'))
      if (!response.ok) return
      const payload = await response.json()
      registrationEnabled = Boolean(payload.registration_enabled)
      if (!registrationEnabled) authMode = 'login'
    } catch {
      // Fail closed: keep registration hidden if the config cannot be read.
    }
  }

  // The dashboard is account-first: every device belongs to the signed-in
  // account. The bearer token is kept only in memory; the HttpOnly session
  // cookie is the durable browser credential.
  async function restoreSession() {
    const storedToken = localStorage.getItem('sync-win-auth-token') || ''
    const storedOwner = localStorage.getItem('sync-win-owner-id') || ''
    const storedEmail = localStorage.getItem('sync-win-account-email') || ''
    const myVersion = ++authVersion
    if (storedToken) localStorage.removeItem('sync-win-auth-token')

    if (storedToken && storedOwner) {
      // Briefly support sessions created by older builds while removing the
      // token from persistent storage immediately.
      authToken = storedToken
      ownerIdentity = storedOwner
      accountEmail = storedEmail
      loadCachedDevices()
    }
    // The dev stack injects a fixed token. Exchange it for a normal session
    // cookie before deciding whether to show the sign-in screen; a refusal
    // falls through to the account flow rather than pretending to be signed in.
    if (devToken) await signInWithDevToken()
    authReady = true

    try {
      const response = await apiFetch(apiURL(`/api/auth/me`), {
        headers: storedToken ? { Authorization: `Bearer ${storedToken}` } : {}
      })
      if (myVersion !== authVersion) return
      if (response.ok) {
        const payload = await response.json()
        ownerIdentity = payload.owner_id
        accountEmail = payload.email
        authToken = ''
        localStorage.setItem('sync-win-owner-id', ownerIdentity)
        localStorage.setItem('sync-win-account-email', accountEmail)
        loadCachedDevices()
        void connectNotifSSE()
        void loadDevices(false)
      } else if (response.status === 401) {
        clearAccountStorage()
        disconnectNotifSSE()
        authToken = ''
        ownerIdentity = ''
        accountEmail = ''
        devices = []
        lastDevicesJson = ''
        resetAccountState()
        sessionExpired = true
      }
    } catch {
      // A network outage must not discard a still-valid local session. The
      // next poll will retry the cookie-backed session.
      if (storedOwner && storedEmail) {
        void loadDevices(false)
        void connectNotifSSE()
      }
    }
  }

  let notifSSE: EventSource | null = null
  let notifConnecting = false
  let notifRetryTimer = 0

  async function connectNotifSSE() {
    if (notifSSE || notifConnecting || !signedIn) return
    notifConnecting = true
    try {
      const response = await apiFetch(apiURL(`/api/notifications/stream-token`), {
        method: 'POST',
        headers: ownerHeaders,
        credentials: 'include'
      })
      if (!response.ok) throw new Error(`stream ticket failed: ${response.status}`)
      const payload = await response.json()
      if (!payload.ticket) throw new Error('stream ticket missing')
      notifSSE = new EventSource(apiURL(`/api/notifications/stream?ticket=${encodeURIComponent(payload.ticket)}`), { withCredentials: true })
      notifSSE.onmessage = (ev) => {
        try {
          const event = JSON.parse(ev.data)
          if (event && event.id) {
            // Advance the cursor too, otherwise the next inbox poll re-fetches
            // and re-appends events that already arrived over SSE.
            if (event.id > lastNotifId) lastNotifId = event.id
            if (!notifEvents.some(e => e.id === event.id)) {
              notifEvents = [...notifEvents, event]
              pushToast(event)
            }
          }
        } catch {}
      }
      notifSSE.onerror = () => {
        notifSSE?.close()
        notifSSE = null
        scheduleNotifReconnect()
      }
    } catch {
      scheduleNotifReconnect()
    } finally {
      notifConnecting = false
    }
  }

  function scheduleNotifReconnect() {
    window.clearTimeout(notifRetryTimer)
    notifRetryTimer = window.setTimeout(() => {
      if (signedIn) void connectNotifSSE()
    }, 5000)
  }

  function disconnectNotifSSE() {
    window.clearTimeout(notifRetryTimer)
    if (notifSSE) {
      notifSSE.close()
      notifSSE = null
    }
  }

  function loadCachedDevices() {
    const raw = cacheRead(`sync-win-devices-${ownerIdentity}`)
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
    localStorage.removeItem('sync-win-auth-token')
    localStorage.removeItem('sync-win-owner-id')
    localStorage.removeItem('sync-win-account-email')
    localStorage.removeItem(`sync-win-devices-${ownerIdentity}`)
  }

  async function loadDevices(showLoading = false) {
    if (devicesLoading) return
    devicesLoading = true
    try {
      if (showLoading && devices.length === 0) loading = true
      const response = await apiFetch(apiURL(`/api/devices`), { headers: ownerHeaders })
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
        // A full localStorage write is synchronous and blocks the main thread.
        // A quota failure here used to land in the catch below and surface as
        // "reconnecting", making a storage problem look like a network outage.
        if (Date.now() - lastCacheWrite > CACHE_WRITE_MS) {
          lastCacheWrite = Date.now()
          cacheWrite(`sync-win-devices-${ownerIdentity}`, serialized)
        }
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
    // While signed out, keep the timer alive but issue no requests so a stale
    // session (logout or expiry) does not poll the API unauthenticated.
    if (!signedIn) {
      schedulePoll(DEVICE_POLL_MS)
      return
    }
    if (document.hidden) {
      liveState = 'paused'
      schedulePoll(DEVICE_POLL_MS)
      return
    }
    await Promise.all([loadDevices(false), pollNotifications()])
    // The summary merge keeps the selected device live; the detail payload is
    // only needed for the heavy fields and is refreshed on its own cadence.
    if (selectedDeviceId && Date.now() - lastDetailLoad >= DETAIL_REFRESH_MS) {
      void loadDeviceDetail(selectedDeviceId)
    }
    schedulePoll(Math.min(DEVICE_POLL_MS * 2 ** Math.min(consecutiveFailures, 4), MAX_POLL_MS))
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

  // localStorage writes are synchronous and can throw on quota or when storage
  // is blocked. A cache write must never be mistaken for a failed fetch, so it
  // is isolated from the request path and never rejects.
  function cacheWrite(key: string, value: string) {
    try {
      localStorage.setItem(key, value)
      return true
    } catch {
      try {
        localStorage.removeItem(key)
      } catch {
        // Storage unavailable; the dashboard still works from live data.
      }
      return false
    }
  }

  function cacheRead(key: string): string | null {
    try {
      return localStorage.getItem(key)
    } catch {
      return null
    }
  }

  async function loadFiles(deviceId: string) {
    try {
      const response = await apiFetch(apiURL(`/api/devices/${deviceId}`), { headers: ownerHeaders })
      if (!response.ok) throw new Error(`file request failed: ${response.status}`)
      const payload = await response.json()
      if (!Array.isArray(payload)) throw new Error('invalid files response')
      filesByDevice = { ...filesByDevice, [deviceId]: payload }
      // Cache to localStorage for instant access on next visit
      cacheWrite(`sync-win-files-${deviceId}`, JSON.stringify(payload))
    } catch (err) {
      // Fallback to cache on error
      const cached = cacheRead(`sync-win-files-${deviceId}`)
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
      const response = await apiFetch(apiURL(`/api/devices/${deviceId}/apps`), { headers: ownerHeaders })
      if (!response.ok) throw new Error(`app request failed: ${response.status}`)
      const payload = await response.json()
      if (!Array.isArray(payload)) throw new Error('invalid app inventory response')
      appsByDevice = { ...appsByDevice, [deviceId]: payload }
      appsLoadedAt = { ...appsLoadedAt, [deviceId]: Date.now() }
      cacheWrite(`sync-win-apps-${deviceId}`, JSON.stringify(payload))
    } catch (err) {
      const message = err instanceof Error ? err.message : 'Could not load app inventory'
      appsError = { ...appsError, [deviceId]: message }
      const cached = cacheRead(`sync-win-apps-${deviceId}`)
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
        const response = await apiFetch(apiURL(`/api/devices/${deviceId}`), { headers: ownerHeaders })
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
      const response = await apiFetch(apiURL(`/api/sync-config`), { headers: ownerHeaders })
      if (!response.ok) throw new Error(`config request failed: ${response.status}`)
      const payload = await response.json()
      extraDirs = Array.isArray(payload.extra_dirs) ? payload.extra_dirs : []
      workspaceDirs = Array.isArray(payload.workspace_dirs) ? payload.workspace_dirs : []
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not load settings', 'error')
    } finally {
      settingsLoading = false
    }
  }

  async function saveSyncConfig() {
    settingsLoading = true
    try {
      const response = await apiFetch(apiURL(`/api/sync-config`), {
        method: 'PUT',
        headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ extra_dirs: extraDirs, workspace_dirs: workspaceDirs })
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

  function addWorkspaceDir() {
    const dir = newWorkspaceDir.trim()
    if (!dir || workspaceDirs.includes(dir)) return
    workspaceDirs = [...workspaceDirs, dir]
    newWorkspaceDir = ''
  }

  function removeWorkspaceDir(dir: string) {
    workspaceDirs = workspaceDirs.filter((item) => item !== dir)
  }

  function openSettings() {
    settingsOpen = true
    void loadSyncConfig()
  }

  function closeSettings() {
    settingsOpen = false
  }

  function openNotifications() {
    userMenuOpen = false
    bellOpen = false
    nav.setSection('notifications')
  }

  function toggleBell() {
    bellOpen = !bellOpen
    if (bellOpen) userMenuOpen = false
  }

  async function pollNotifications() {
    if (!signedIn) return
    if (!notifPrefsLoaded) {
      notifPrefsLoaded = true
      void loadNotificationPrefs(ownerHeaders)
    }
    try {
      const res = await apiFetch(apiURL(`/api/notifications/inbox?since=${lastNotifId}`), { headers: ownerHeaders })
      if (!res.ok) return
      const events = await res.json()
      if (Array.isArray(events) && events.length > 0) {
        for (const e of events) {
          if (e.id > lastNotifId) lastNotifId = e.id
          if (!e.read && !notifEvents.some(existing => existing.id === e.id)) {
            notifEvents = [...notifEvents, e]
            // Only an event that just happened is worth interrupting for. The
            // history the dashboard reads on open is older than that, so it
            // lands in the inbox and in the bell without a toast.
            pushToast(e)
          }
        }
      }
    } catch {}
  }

  // pushToast decides whether an event interrupts, once, when it arrives.
  //
  // The rules: the in-app alerts are on, the event is one the person chose to
  // be interrupted by, and it happened moments ago. Two events of the same
  // kind about the same device replace each other instead of stacking, so a
  // flapping agent cannot fill the screen.
  function pushToast(event: { id: number; type: string; device_id: string; hostname: string; message: string; created_at: string }) {
    if (!shouldToast($notificationPrefs, event)) return
    if (toasts.some((t) => t.id === event.id)) return
    const kept = toasts.filter((t) => !(t.type === event.type && t.device_id === event.device_id))
    // Three at a time: the newest is the one that matters, and the rest are
    // in the inbox.
    toasts = [...kept, event].slice(-3)
  }

  function dismissToast(id: number) {
    toasts = toasts.filter((t) => t.id !== id)
  }

  function dismissNotif(id: number) {
    notifEvents = notifEvents.filter((e) => e.id !== id)
    toasts = toasts.filter((t) => t.id !== id)
    void apiFetch(apiURL(`/api/notifications/inbox/read`), {
      method: 'POST',
      headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids: [id] }),
    })
  }

  // The bell lists unread events; "mark all read" must cover the history the
  // client has not loaded yet, so it uses the server-side all flag.
  function clearNotifs() {
    notifEvents = []
    toasts = []
    void apiFetch(apiURL(`/api/notifications/inbox/read`), {
      method: 'POST',
      headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
      body: JSON.stringify({ all: true }),
    })
  }

  function dismissToasts() {
    const ids = toasts.map((t) => t.id)
    toasts = []
    if (ids.length === 0) return
    notifEvents = notifEvents.filter((e) => !ids.includes(e.id))
    void apiFetch(apiURL(`/api/notifications/inbox/read`), {
      method: 'POST',
      headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids }),
    })
  }

  // The notification centre marks events read; keep the bell in step without
  // refetching the whole inbox.
  function handleNotifMarkedRead(ids: number[], all: boolean) {
    if (all) {
      notifEvents = []
      toasts = []
      return
    }
    const marked = new Set(ids)
    notifEvents = notifEvents.filter((e) => !marked.has(e.id))
    toasts = toasts.filter((e) => !marked.has(e.id))
  }

  // Selecting a device navigates the shell to its overview. No popup: the same
  // gesture used to open a modal, but the panel now owns the whole main area.
  function selectDevice(deviceId: string) {
    if (!deviceId) return
    nav.selectDevice(deviceId)
    mobileNavOpen = false
  }

  // openNotifDevice takes the person to the device the event is about and
  // clears the event. A notification that does not lead anywhere is noise, so
  // the toast and the bell row are both a way in.
  function openNotifDevice(event: { id: number; device_id: string }) {
    if (event.device_id) {
      selectDevice(event.device_id)
    }
    bellOpen = false
    dismissNotif(event.id)
  }

  async function loadDeviceDetail(deviceId: string, includePosture = false) {
    if (!deviceId) return
    lastDetailLoad = Date.now()
    try {
      const response = await apiFetch(apiURL(`/api/devices/${deviceId}/detail`), { headers: ownerHeaders })
      if (!response.ok) return
      const payload = await response.json()
      if (!payload || typeof payload !== 'object' || Array.isArray(payload) || !payload.id) return
      // Preserve any posture already stored. The poll calls this without
      // includePosture, and replacing the entry wholesale wiped the posture the
      // Security section had just fetched, so the screen lost its listeners and
      // failed units within one poll.
      const existing = deviceDetails[deviceId] as any
      const entry = includePosture
        ? { ...payload, securityPosture: await fetchPosture(deviceId) }
        : { ...payload, securityPosture: existing?.securityPosture }
      deviceDetails = { ...deviceDetails, [deviceId]: entry as Device }
    } catch {
      // Keep the last known detail; the list summary still renders.
    }
  }

  // Posture for the Security screen: public listeners, pending updates and
  // failed units. Returns the value; the caller stores it on the device detail.
  async function fetchPosture(deviceId: string): Promise<SecurityPosture> {
    const get = async (path: string) => {
      const res = await apiFetch(apiURL(`/api/devices/${deviceId}${path}`), { headers: ownerHeaders })
      if (!res.ok) throw new Error(`${path} failed (${res.status})`)
      return res.json()
    }
    const [portsRes, updatesRes, servicesRes] = await Promise.allSettled([
      get('/ports'),
      get('/updates'),
      get('/services')
    ])
    const next: SecurityPosture = {
      publicPorts: [],
      pendingUpdates: 0,
      pendingStatus: '',
      failedServices: [],
      loaded: true,
      error: ''
    }
    if (portsRes.status === 'fulfilled' && Array.isArray(portsRes.value)) {
      next.publicPorts = uniquePorts(
        portsRes.value.filter((p: any) => ['', '0.0.0.0', '::', '*', '[::]'].includes(p.local_address || ''))
      ).sort((a: any, b: any) => a.port - b.port)
    }
    if (updatesRes.status === 'fulfilled' && updatesRes.value) {
      next.pendingStatus = updatesRes.value.status || ''
      next.pendingUpdates = Array.isArray(updatesRes.value.updates) ? updatesRes.value.updates.length : 0
    }
    if (servicesRes.status === 'fulfilled' && Array.isArray(servicesRes.value)) {
      next.failedServices = servicesRes.value.filter((s: any) => s.status === 'failed').map((s: any) => s.name)
    }
    const failed = [portsRes, updatesRes, servicesRes].filter((r) => r.status === 'rejected')
    next.error = failed.length
      ? `Some posture data could not be read (${(failed[0] as PromiseRejectedResult).reason?.message || 'request failed'})`
      : ''
    return next
  }

  async function refreshDeviceSettings() {
    if (!modalDevice) return
    const deviceId = modalDevice.id
    await Promise.all([loadDevices(), loadDeviceDetail(deviceId)])
  }

  // The topbar refresh has to force the detail too; reloading only the summary
  // left the selected screen looking frozen.
  function refreshNow() {
    void loadDevices(true)
    if (selectedDeviceId) {
      lastDetailLoad = 0
      void loadDeviceDetail(selectedDeviceId, navState.section === 'security')
    }
  }

  // Topbar range selector. It only publishes the chart window now: the device
  // poll is fixed so a wide window cannot make a healthy device look stale.
  // DeviceOverview owns the history refresh cadence for that window.
  function setRange(id: RangeId) {
    currentRange = id
    selectedRange.set(id)
  }

  // Global search (Ctrl+K). Until a dedicated command palette exists, this
  // focuses the fleet search field so the shortcut still does something useful.
  function openGlobalSearch() {
    nav.setSection('home')
    tick().then(() => {
      const el = document.querySelector<HTMLInputElement>('.panel-inner .search-input, .content .search-input')
      el?.focus()
    })
  }

  const SECTION_TITLES: Record<string, string> = {
    home: 'Fleet',
    devices: 'Devices',
    alerts: 'Alerts',
    findings: 'Findings',
    reports: 'Reports',
    notifications: 'Notifications',
    configuration: 'Settings'
  }

  function sectionTitleFor(section: Section): string {
    return SECTION_TITLES[section] || 'Fleet'
  }

  // A device can disappear while it is selected (deleted, or filtered out).
  // Drop the selection so the shell falls back to the fleet instead of
  // rendering a page for a device that no longer exists.
  function handleDeviceRemoved(deviceId: string) {
    devices = devices.filter((d) => d.id !== deviceId)
    delete filesByDevice[deviceId]
    if (navState.deviceId === deviceId) nav.clearDevice()
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
    localStorage.setItem('sync-win-install-open', installOpen ? '1' : '0')
    if (installOpen) {
      enrollmentAttempted = false
      if (!enrollmentToken && signedIn) {
        requestEnrollmentToken()
      }
    }
  }

  async function requestEnrollmentToken() {
    if (!signedIn || enrollmentLoading) return
    enrollmentAttempted = true
    enrollmentLoading = true
    enrollmentError = ''
    try {
      const res = await apiFetch(apiURL(`/api/agent/enroll-token`), {
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
    enrollmentAttempted = false
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
      const response = await apiFetch(apiURL(`/api/devices/${restoreSourceDevice.id}/restore-saves`), {
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
      const response = await apiFetch(apiURL(`/api/devices/${deviceId}/apps?action=install`), {
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
      const response = await apiFetch(apiURL(`/api/devices/${deviceId}/files/${file.id}`), {
        method: 'DELETE', headers: ownerHeaders
      })
      if (!response.ok) throw new Error(`request failed: ${response.status}`)
      notify('File removed. The agent will apply the exclusion on its next cycle.')
      await loadFiles(deviceId)
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not exclude file', 'error')
    }
  }

  // Removal from the Settings screen. The confirmation already happened in that
  // screen's dialog (with typed hostname), so this skips the extra
  // window.confirm that deleteDevice() shows.
  async function removeDeviceConfirmed(device: Device) {
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}`), { method: 'DELETE', headers: ownerHeaders })
      if (!response.ok) throw new Error(`request failed: ${response.status}`)
      handleDeviceRemoved(device.id)
      nav.clearDevice()
      notify('Device removed.')
    } catch (err) {
      notify(err instanceof Error ? err.message : 'Could not remove device', 'error')
    }
  }

  async function deleteDevice(device: Device) {
    if (!window.confirm(`Remove ${device.hostname} and all synchronized data?`)) return
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}`), { method: 'DELETE', headers: ownerHeaders })
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
      if (authMode === 'register' && !registrationEnabled) {
        authError = 'Registration is disabled.'
        return
      }
      const response = await apiFetch(apiURL(`/api/auth/${authMode}`), {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password })
      })
      const payload = await response.json().catch(() => ({}))
      if (!response.ok) throw new Error(payload.error || `Request failed (${response.status})`)
      // Bump auth version so any in-flight restoreSession auth/me won't overwrite us
      authVersion++
      authToken = ''
      ownerIdentity = payload.owner_id
      accountEmail = payload.email
      sessionExpired = false
      localStorage.removeItem('sync-win-auth-token')
      localStorage.setItem('sync-win-owner-id', ownerIdentity)
      localStorage.setItem('sync-win-account-email', accountEmail)
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

  // resetAccountState clears everything derived from the signed-in account so a
  // different account on the same browser never sees the previous one's
  // notifications, cached panels or telemetry.
  function resetAccountState() {
    notifEvents = []
    toasts = []
    lastNotifId = 0
    notifPrefsLoaded = false
    resetNotificationPrefs()
    deviceDetails = {}
    filesByDevice = {}
    appsByDevice = {}
    savesByDevice = {}
    activeTab = {}
    appsLoadedAt = {}
    expandedGroups = {}
    selectedDeviceId = ''
    lastDetailLoad = 0
    resetTelemetry()
  }

  function signOut() {
    void apiFetch(apiURL(`/api/auth/logout`), { method: 'POST', headers: ownerHeaders }).catch(() => {})
    clearAccountStorage()
    disconnectNotifSSE()
    authToken = ''
    accountEmail = ''
    ownerIdentity = ''
    devices = []
    lastDevicesJson = ''
    error = ''
    loading = false
    resetAccountState()
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
      const response = await apiFetch(apiURL(`/api/auth/update-email`), {
        method: 'POST',
        headers: { ...ownerHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ email: profileEmail })
      })
      const payload = await response.json().catch(() => ({}))
      if (!response.ok) throw new Error(payload.error || `Request failed (${response.status})`)
      accountEmail = payload.email
      localStorage.setItem('sync-win-account-email', accountEmail)
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
      const response = await apiFetch(apiURL(`/api/auth/update-password`), {
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

  // The application logs live in the fleet-wide Settings screen, which is the
  // single viewer. The menu item deep-links there instead of opening a second,
  // duplicate modal.
  function openAppLogs() {
    userMenuOpen = false
    nav.setSection('configuration')
  }

  function openAddDevice() {
    addDeviceOpen = true
    if (signedIn && !enrollmentToken) {
      enrollmentAttempted = false
      requestEnrollmentToken()
    }
  }

  function closeAddDevice() {
    addDeviceOpen = false
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


  // Both delegate to lib/format so the Go zero time collapses to "never"
  // instead of reporting an age of ~739893 days.
  const timeAgo = formatRelative
  const formatTime = formatAbsolute

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


  function categoryChip(category: string) {
    let hash = 0
    for (let i = 0; i < category.length; i++) hash = (hash * 31 + category.charCodeAt(i)) >>> 0
    const hue = hash % 360
    return `background: hsl(${hue}, 45%, 16%); border-color: hsl(${hue}, 55%, 32%); color: hsl(${hue}, 85%, 80%)`
  }

  onMount(() => {
    void loadAuthConfig()
    void restoreSession()
    // Browsers keep an AudioContext suspended until a user gesture. Unlock it
    // on the first interaction so the first notification is not silent.
    const unlockAudio = () => {
      unlockNotificationAudio()
      window.removeEventListener('pointerdown', unlockAudio)
      window.removeEventListener('keydown', unlockAudio)
    }
    window.addEventListener('pointerdown', unlockAudio)
    window.addEventListener('keydown', unlockAudio)
    schedulePoll(DEVICE_POLL_MS)
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
      window.removeEventListener('pointerdown', unlockAudio)
      window.removeEventListener('keydown', unlockAudio)
      document.removeEventListener('visibilitychange', onVisibility)
      document.removeEventListener('click', handleClickOutside)
      disconnectNotifSSE()
    }
  })
</script>

<svelte:head>
  <title>SyncWin Dashboard</title>
</svelte:head>

<svelte:window on:keydown={handleWindowKeydown} />

<div class="page" class:full={signedIn}>
  {#if !authReady}
    <div class="auth-gate">
      <div class="auth-card">
        <p class="eyebrow">SyncWin</p>
        <h1>Connecting…</h1>
      </div>
    </div>
  {:else if !signedIn}
    <div class="auth-gate">
      <div class="auth-card" role="dialog" aria-labelledby="auth-gate-title">
        <p class="eyebrow">SyncWin</p>
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
          {#if registrationEnabled}
            <button type="button" class="secondary" on:click={() => (authMode = authMode === 'login' ? 'register' : 'login')}>
              {authMode === 'login' ? 'New here? Create account' : 'Already have an account? Sign in'}
            </button>
          {/if}
        </form>
      </div>
    </div>
  {:else}
  <div class="app-shell">
    {#if mobileNavOpen}
      <div class="nav-scrim" role="presentation" on:click={() => (mobileNavOpen = false)}></div>
    {/if}
    <div class="sidebar-wrap" class:open={mobileNavOpen}>
      <Sidebar
        {devices}
        selectedId={navState.deviceId}
        activeSection={navState.section}
        collapsed={sidebarCollapsed}
        onSelectDevice={selectDevice}
        onNavigate={navigateTo}
        onAddDevice={openAddDevice}
        onToggleCollapse={toggleSidebar}
      />
    </div>
    <div class="main-col">
      <Topbar
        device={isDeviceSection ? modalDevice : null}
        sectionTitle={sectionTitleFor(navState.section)}
        rangeId={currentRange}
        onRangeChange={setRange}
        onManualRefresh={refreshNow}
        onOpenSearch={openGlobalSearch}
        onOpenNotifications={toggleBell}
        onOpenUserMenu={toggleUserMenu}
        accountInitial={accountEmail.charAt(0).toUpperCase()}
        unreadCount={notifEvents.length}
        onOpenMobileNav={() => (mobileNavOpen = true)}
      />

      <!-- Popovers for the topbar buttons. They live here rather than inside
           Topbar because they need App-level state (notifications, profile
           forms). The wrapper classes are what handleClickOutside watches, so
           clicking away closes them. -->
      {#if bellOpen}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="shell-popover bell-wrapper" role="presentation" on:click|stopPropagation on:keydown|stopPropagation>
          <div class="bell-dropdown">
            <div class="bell-header">
              <span class="bell-title">Notifications {#if notifEvents.length > 0}<span class="bell-count">{notifEvents.length}</span>{/if}</span>
              {#if notifEvents.length > 0}
                <button class="bell-clear" on:click={clearNotifs}>Mark all read</button>
              {/if}
            </div>
            <div class="bell-list">
              {#if notifEvents.length === 0}
                <div class="bell-empty">No new notifications</div>
              {:else}
                {#each notifEvents.slice(-5) as e (e.id)}
                  <button class="bell-item bell-{e.type}" on:click={() => openNotifDevice(e)}>
                    <div class="bell-item-top">
                      <span class="bell-item-icon">{eventIcon(e.type)}</span>
                      <span class="bell-item-host">{e.hostname || e.device_id}</span>
                      <span class="bell-item-time">{timeAgo(e.created_at, now)}</span>
                    </div>
                    <div class="bell-item-msg">{eventDetail(e.message)}</div>
                  </button>
                {/each}
              {/if}
            </div>
            <div class="bell-footer">
              <button class="bell-settings" on:click={openNotifications}>Open notification center</button>
            </div>
          </div>
        </div>
      {/if}

      {#if userMenuOpen}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="shell-popover user-menu-wrapper" role="presentation" on:click|stopPropagation on:keydown|stopPropagation>
          <div class="user-dropdown">
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
            <button class="dropdown-item" on:click={openAppLogs}>
              <span class="dropdown-icon">📋</span> Logs
            </button>
            <button class="dropdown-item" on:click={openNotifications}>
              <span class="dropdown-icon">🔔</span> Notifications
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
        </div>
      {/if}
      <main class="content">
        <div class="panel-inner">
          {#if toastMessage}
            <div class="toast {toastKind}" role="status">{toastMessage}</div>
          {/if}

          {#if isDeviceSection && modalDevice}
            {#if navState.section === 'storage'}
              <Storage device={modalDevice} />
            {:else if navState.section === 'network'}
              <Network device={modalDevice} />
            {:else if navState.section === 'containers'}
              <Docker device={modalDevice} authHeaders={ownerHeaders} />
            {:else if navState.section === 'security'}
              <Security
                device={modalDevice}
                authHeaders={ownerHeaders}
                posture={activeSecurityPosture}
                lynisAvailable={modalDevice.hardware?.lynis_available ?? false}
                lynisInstallCmd={modalDevice.hardware?.lynis_install_cmd ?? 'sudo apt install lynis'}
              />
            {:else if navState.section === 'processes'}
              <Processes device={modalDevice} />
            {:else if navState.section === 'packages'}
              <Packages deviceId={modalDevice.id} authHeaders={ownerHeaders} />
            {:else if navState.section === 'services'}
              <Services deviceId={modalDevice.id} />
            {:else if navState.section === 'logs'}
              <DeviceLogs deviceId={modalDevice.id} authHeaders={ownerHeaders} />
            {:else if navState.section === 'remote'}
              <RemoteAccess device={modalDevice} authHeaders={ownerHeaders} />
            {:else if navState.section === 'settings'}
              <DeviceSettings device={modalDevice} authHeaders={ownerHeaders} onRemove={removeDeviceConfirmed} on:saved={refreshDeviceSettings} />
            {:else if navState.section === 'cpu'}
              <CpuThermal device={modalDevice} />
            {:else if navState.section === 'memory'}
              <Memory device={modalDevice} />
            {:else if navState.section === 'sensors'}
              <Sensors device={modalDevice} />
            {:else if isHardwareSection}
              <DeviceOverview device={modalDevice} authHeaders={ownerHeaders} />
            {:else}
              <DeviceModal
                device={modalDevice}
                open={true}
                variant="page"
                activeTabName={deviceTabForSection}
                authHeaders={ownerHeaders}
                initialFiles={filesByDevice[modalDevice.id] || []}
                on:close={() => nav.clearDevice()}
                on:removed={() => handleDeviceRemoved(modalDevice.id)}
                on:restore={(event) => restoreGameSaves(event.detail.deviceId, event.detail.prefixId, event.detail.gameName)}
              />
            {/if}
          {:else if navState.section === 'alerts'}
            <Alerts {devices} authHeaders={ownerHeaders} onSelect={selectDevice} />
          {:else if navState.section === 'findings'}
            <Findings {devices} onSelect={selectDevice} />
          {:else if navState.section === 'reports'}
            <Reports {devices} onSelect={selectDevice} />
          {:else if navState.section === 'configuration'}
            <AppSettings {devices} authHeaders={ownerHeaders} />
          {:else if navState.section === 'notifications'}
            <Notifications {devices} authHeaders={ownerHeaders} unread={notifEvents} onSelect={selectDevice} onMarkedRead={handleNotifMarkedRead} />
          {:else}
            <Home {devices} onSelect={selectDevice} onAdd={openAddDevice} onAlerts={() => nav.setSection('alerts')} />
          {/if}
        </div>
      </main>
    </div>
  </div>
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

      <section class="settings-section">
        <h3>Workspace projects</h3>
        <p class="muted">Collect only .vscode/settings.json, tasks.json and launch.json from these project roots.</p>
        <form class="extra-dirs-form" on:submit|preventDefault={addWorkspaceDir}>
          <input type="text" bind:value={newWorkspaceDir} placeholder="/path/to/project" disabled={settingsLoading} />
          <button type="submit" disabled={settingsLoading || !newWorkspaceDir.trim()}>Add</button>
        </form>
        {#if workspaceDirs.length === 0}
          <p class="muted">No workspace projects configured.</p>
        {:else}
          <ul class="extra-dirs-list">
            {#each workspaceDirs as dir (dir)}
              <li>
                <code>{dir}</code>
                <button class="inline danger" on:click={() => removeWorkspaceDir(dir)}>Remove</button>
              </li>
            {/each}
          </ul>
        {/if}
        <div class="settings-actions">
          <button on:click={saveSyncConfig} disabled={settingsLoading}>Save configuration</button>
        </div>
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

{#if addDeviceOpen}
  <div class="modal-backdrop" role="presentation" on:click={closeAddDevice}>
    <div class="install-modal" role="dialog" aria-modal="true" aria-labelledby="install-modal-title" tabindex="-1" on:click|stopPropagation on:keydown|stopPropagation>
      <div class="modal-header">
        <div>
          <p class="eyebrow">Installation</p>
          <h2 id="install-modal-title">Add a Linux device</h2>
          <small>Run this once on the target machine. A temporary enrollment token is generated for each install.</small>
        </div>
        <button class="ghost" on:click={closeAddDevice}>Close</button>
      </div>

      {#if installServerIsLocal}
        <div class="warn-banner" role="alert">
          <strong>Dashboard accessed via localhost — this command only works on this machine.</strong>
          To install on another device, replace the address below with the server's reachable address
          (e.g. <code>http://100.x.x.x:8080</code>).
        </div>
      {/if}

      <div class="install-section">
        <span class="section-label">Capabilities</span>
        <div class="cap-toggles">
          <label class="toggle-row">
            <input type="checkbox" bind:checked={enableSsh} />
            <span class="toggle-text">
              <strong>SSH remote access</strong>
              <em>Open a terminal from this dashboard. No port is opened on the device.</em>
            </span>
          </label>
          <label class="toggle-row">
            <input type="checkbox" bind:checked={enableAutoUpdate} />
            <span class="toggle-text">
              <strong>Automatic updates</strong>
              <em>Keep the agent current. Turning this off means the device stops receiving fixes.</em>
            </span>
          </label>
        </div>
      </div>

      <div class="install-section">
        <label class="server-address-row">
          <span class="section-label">Agent connects to</span>
          <input type="text" bind:value={installServer} spellcheck="false" aria-label="Server address used by installed agents" />
        </label>
        <small class="install-hint">
          Dev stack: API on host port <strong>8088</strong>, server container on <strong>8080</strong>, dashboard on <strong>5173</strong>.
        </small>
      </div>

      {#if enrollmentLoading}
        <div class="command-box">
          <pre>Generating enrollment token…</pre>
        </div>
      {:else if enrollmentError}
        <div class="command-box">
          <pre class="error">{enrollmentError}</pre>
          <button on:click={refreshInstallToken}>Retry</button>
        </div>
      {:else if installCommand}
        <div class="command-box">
          <pre>{installCommand}</pre>
          <div class="command-actions">
            <button on:click={() => copyText(installCommand)}>Copy command</button>
            <button class="ghost" on:click={refreshInstallToken}>New token</button>
          </div>
        </div>
      {/if}

      <details class="change-later">
        <summary>Change these later</summary>
        <div class="cap-row">
          <div class="cap-text">
            <strong>SSH remote access</strong>
            <code>sudo sync-win-agent set ssh on</code>
          </div>
          <button class="ghost" on:click={() => copyText('sudo sync-win-agent set ssh on')}>Copy</button>
        </div>
        <div class="cap-row">
          <div class="cap-text">
            <strong>Automatic updates</strong>
            <code>sudo sync-win-agent update</code>
          </div>
          <button class="ghost" on:click={() => copyText('sudo sync-win-agent update')}>Copy</button>
        </div>
        <p class="muted">The agent never changes a capability on its own.</p>
      </details>
    </div>
  </div>
{/if}

{#if toasts.length > 0}
  <div class="notif-toast-stack">
    <div class="notif-toast-header">
      <strong>Alerts</strong>
      <button class="inline" on:click={dismissToasts}>Dismiss</button>
    </div>
    {#each toasts as evt (evt.id)}
      <NotificationToast
        event={evt}
        sound={$notificationPrefs.sound}
        volume={$notificationPrefs.volume}
        on:dismiss={() => dismissToast(evt.id)}
        on:open={() => openNotifDevice(evt)}
      />
    {/each}
    {#if notifEvents.length > toasts.length}
      <button class="notif-toast-more" on:click={openNotifications}>
        {notifEvents.length - toasts.length} more in the notification center
      </button>
    {/if}
  </div>
{/if}
