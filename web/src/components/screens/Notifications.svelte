<script lang="ts">
  import { onDestroy, onMount } from 'svelte'
  import { get } from 'svelte/store'
  import { apiFetch, apiURL } from '../../lib/api'
  import { deviceLabel, type Device } from '../../lib/types'
  import { formatRelative, parseTimestamp } from '../../lib/format'
  import {
    DEFAULT_NOTIFICATION_PREFS,
    loadNotificationPrefs,
    notificationPrefs,
    saveInAppPrefs,
    type NotificationPrefs
  } from '../../lib/notification-prefs'
  import { NOTIFICATION_SOUNDS, playNotificationSound } from '../../lib/notification-sound'
  import EmptyState from '../ui/EmptyState.svelte'
  import SeverityBadge from '../ui/SeverityBadge.svelte'

  export let devices: Device[] = []
  export let authHeaders: Record<string, string> = {}
  export let unread: NotificationEvent[] = []
  export let onSelect: (id: string) => void = () => {}
  export let onMarkedRead: (ids: number[], all: boolean) => void = () => {}

  type NotificationEvent = {
    id: number
    type: string
    device_id: string
    hostname: string
    message: string
    created_at: string
    read?: boolean
  }

  type EventInfo = { type: string; label: string; description: string; severity: string }
  type ProviderConfig = {
    provider: string
    enabled: boolean
    events: string[]
    config: { sound?: string; volume?: number; events_set?: boolean; chat_id?: string; url?: string } & Record<string, unknown>
    configured: boolean
  }
  type Tab = 'inbox' | 'preferences'

  const FALLBACK_EVENTS: EventInfo[] = [
    { type: 'device_offline', label: 'Device goes offline', description: 'The agent stopped reporting.', severity: 'crit' },
    { type: 'device_online', label: 'Device comes back online', description: 'The agent reported again.', severity: 'good' },
    { type: 'sync_error', label: 'Sync error', description: 'The agent reported a sync failure.', severity: 'warn' },
    { type: 'device_enrolled', label: 'New device enrolled', description: 'A new device registered.', severity: 'info' }
  ]

  let tab: Tab = 'inbox'
  let history: NotificationEvent[] = []
  let loading = true
  let refreshing = false
  let error = ''
  let filterType = 'all'
  let filterDevice = 'all'
  let unreadOnly = false
  let catalog: EventInfo[] = [...FALLBACK_EVENTS]

  let prefs: NotificationPrefs = { ...DEFAULT_NOTIFICATION_PREFS }
  let form: NotificationPrefs = { ...DEFAULT_NOTIFICATION_PREFS }
  let prefsNote = ''
  let prefsError = ''
  let savingPrefs = false
  let formDirty = false
  const unsubPrefs = notificationPrefs.subscribe((value) => {
    prefs = value
  })

  let telegramEnabled = false
  let telegramToken = ''
  let telegramChatId = ''
  let telegramEvents: string[] = ['device_offline', 'sync_error']
  let webhookEnabled = false
  let webhookUrl = ''
  let webhookSecret = ''
  let webhookEvents: string[] = ['device_offline', 'sync_error']

  let savingProvider = ''
  let testing = ''
  let detecting = false
  let telegramNote = ''
  let telegramError = ''
  let webhookNote = ''
  let webhookError = ''

  onMount(() => {
    void loadCatalog()
    void loadHistory()
    void loadPrefs()
    void loadProviders()
  })

  onDestroy(unsubPrefs)

  async function loadCatalog() {
    try {
      const res = await apiFetch(apiURL('/api/notifications/events'), { headers: authHeaders })
      if (!res.ok) return
      const data = await res.json()
      if (Array.isArray(data) && data.length > 0) catalog = data
    } catch {
      // Keep the fallback copy; preferences must still render.
    }
  }

  async function loadHistory() {
    refreshing = true
    error = ''
    try {
      const res = await apiFetch(apiURL('/api/notifications/inbox?limit=100'), { headers: authHeaders })
      if (!res.ok) throw new Error(`request failed (${res.status})`)
      const data = await res.json()
      history = Array.isArray(data) ? data : []
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not load notifications'
    } finally {
      loading = false
      refreshing = false
    }
  }

  async function loadPrefs() {
    await loadNotificationPrefs(authHeaders)
    // A fast click on a preference can land before this request returns; never
    // overwrite the user's edit with the value that was read first.
    if (!formDirty) form = { ...get(notificationPrefs), events: [...get(notificationPrefs).events] }
  }

  async function loadProviders() {
    try {
      const res = await apiFetch(apiURL('/api/notifications/config'), { headers: authHeaders })
      if (!res.ok) return
      const configs: ProviderConfig[] = await res.json()
      for (const cfg of configs) {
        if (cfg.provider === 'telegram') {
          telegramEnabled = cfg.enabled
          telegramEvents = cfg.events?.length ? [...cfg.events] : telegramEvents
          telegramChatId = String(cfg.config?.chat_id ?? '')
        }
        if (cfg.provider === 'webhook') {
          webhookEnabled = cfg.enabled
          webhookEvents = cfg.events?.length ? [...cfg.events] : webhookEvents
          webhookUrl = String(cfg.config?.url ?? '')
        }
      }
    } catch {
      // Provider forms are optional; the in-app section still works.
    }
  }

  function mergeEvents(stored: NotificationEvent[], live: NotificationEvent[]): NotificationEvent[] {
    const byId = new Map<number, NotificationEvent>()
    for (const event of stored) byId.set(event.id, { ...event })
    for (const event of live) {
      const existing = byId.get(event.id)
      byId.set(event.id, { ...existing, ...event, read: false })
    }
    return [...byId.values()].sort((a, b) => b.id - a.id)
  }

  $: merged = mergeEvents(history, unread)
  $: unreadCount = merged.filter((e) => !e.read).length
  $: visible = merged.filter((e) => {
    if (unreadOnly && e.read) return false
    if (filterType !== 'all' && e.type !== filterType) return false
    if (filterDevice !== 'all' && e.device_id !== filterDevice) return false
    return true
  })
  $: groups = groupByDay(visible)

  // The history is a diary, so it is read by day. A flat list of a hundred
  // rows is a wall; "Today" and "Yesterday" are the two anchors a person
  // looks for.
  function groupByDay(events: NotificationEvent[]): { label: string; events: NotificationEvent[] }[] {
    const out: { label: string; events: NotificationEvent[] }[] = []
    for (const event of events) {
      const label = dayLabel(event.created_at)
      const last = out[out.length - 1]
      if (last && last.label === label) {
        last.events.push(event)
        continue
      }
      out.push({ label, events: [event] })
    }
    return out
  }

  function dayLabel(stamp: string): string {
    const parsed = parseTimestamp(stamp)
    if (parsed == null) return 'Unknown date'
    const day = new Date(parsed)
    const today = new Date()
    if (day.toDateString() === today.toDateString()) return 'Today'
    const yesterday = new Date(today.getTime() - 86_400_000)
    if (day.toDateString() === yesterday.toDateString()) return 'Yesterday'
    return day.toLocaleDateString([], { day: '2-digit', month: 'short', year: 'numeric' })
  }

  function infoFor(type: string): EventInfo {
    return catalog.find((e) => e.type === type) ?? FALLBACK_EVENTS.find((e) => e.type === type) ?? {
      type,
      label: type,
      description: '',
      severity: 'info'
    }
  }

  function iconFor(type: string): string {
    switch (type) {
      case 'device_offline': return '🔴'
      case 'device_online': return '🟢'
      case 'sync_error': return '⚠️'
      case 'device_enrolled': return '➕'
      default: return 'ℹ️'
    }
  }

  async function markRead(event: NotificationEvent) {
    try {
      const res = await apiFetch(apiURL('/api/notifications/inbox/read'), {
        method: 'POST',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ ids: [event.id] })
      })
      if (!res.ok) throw new Error(`request failed (${res.status})`)
      history = history.map((e) => (e.id === event.id ? { ...e, read: true } : e))
      onMarkedRead([event.id], false)
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not mark the notification as read'
    }
  }

  async function markAllRead() {
    try {
      const res = await apiFetch(apiURL('/api/notifications/inbox/read'), {
        method: 'POST',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ all: true })
      })
      if (!res.ok) throw new Error(`request failed (${res.status})`)
      history = history.map((e) => ({ ...e, read: true }))
      onMarkedRead([], true)
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not mark everything as read'
    }
  }

  function toggleEvent(list: string[], event: string): string[] {
    return list.includes(event) ? list.filter((e) => e !== event) : [...list, event]
  }

  // The in-app card edits the popup list, not the subscription list: every
  // event is recorded in the inbox, and only the ones chosen here interrupt.
  function togglePopup(event: string) {
    formDirty = true
    form = { ...form, popups: toggleEvent(form.popups, event) }
  }

  function testSound() {
    playNotificationSound(form.sound, form.volume)
  }

  async function savePrefs() {
    savingPrefs = true
    prefsNote = ''
    prefsError = ''
    try {
      await saveInAppPrefs(form, authHeaders)
      formDirty = false
      form = { ...get(notificationPrefs), events: [...get(notificationPrefs).events] }
      prefsNote = 'Saved. The new sound and event selection apply immediately.'
    } catch (e) {
      prefsError = e instanceof Error ? e.message : 'Could not save the in-app preferences'
    } finally {
      savingPrefs = false
    }
  }

  async function saveTelegram() {
    savingProvider = 'telegram'
    telegramNote = ''
    telegramError = ''
    try {
      const body: Record<string, unknown> = {
        provider: 'telegram',
        enabled: telegramEnabled,
        events: telegramEvents,
        config: { chat_id: telegramChatId }
      }
      if (telegramToken && !telegramToken.includes('*')) {
        ;(body.config as Record<string, unknown>).bot_token = telegramToken
      }
      const res = await apiFetch(apiURL('/api/notifications/config'), {
        method: 'PUT',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      })
      if (!res.ok) {
        const payload = await res.json().catch(() => ({}))
        throw new Error(payload.error || `request failed (${res.status})`)
      }
      telegramNote = 'Telegram configuration saved.'
    } catch (e) {
      telegramError = e instanceof Error ? e.message : 'Could not save Telegram'
    } finally {
      savingProvider = ''
    }
  }

  async function saveWebhook() {
    savingProvider = 'webhook'
    webhookNote = ''
    webhookError = ''
    try {
      const body: Record<string, unknown> = {
        provider: 'webhook',
        enabled: webhookEnabled,
        events: webhookEvents,
        config: { url: webhookUrl }
      }
      if (webhookSecret && !webhookSecret.includes('*')) {
        ;(body.config as Record<string, unknown>).secret = webhookSecret
      }
      const res = await apiFetch(apiURL('/api/notifications/config'), {
        method: 'PUT',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify(body)
      })
      if (!res.ok) {
        const payload = await res.json().catch(() => ({}))
        throw new Error(payload.error || `request failed (${res.status})`)
      }
      webhookNote = 'Webhook configuration saved.'
    } catch (e) {
      webhookError = e instanceof Error ? e.message : 'Could not save the webhook'
    } finally {
      savingProvider = ''
    }
  }

  async function detectChatId() {
    if (!telegramToken || telegramToken.includes('*')) {
      telegramError = 'Enter the bot token first.'
      return
    }
    detecting = true
    telegramError = ''
    try {
      const res = await apiFetch(apiURL('/api/notifications/telegram/detect-chat'), {
        method: 'POST',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ bot_token: telegramToken })
      })
      const data = await res.json()
      if (res.ok && data.chat_id) {
        telegramChatId = data.chat_id
        telegramNote = `Chat ID detected: ${data.chat_id}`
      } else {
        telegramError = data.error || 'Could not detect the chat ID. Send /start to the bot first.'
      }
    } catch {
      telegramError = 'Could not detect the chat ID.'
    } finally {
      detecting = false
    }
  }

  async function testProvider(provider: string) {
    testing = provider
    if (provider === 'telegram') { telegramNote = ''; telegramError = '' } else { webhookNote = ''; webhookError = '' }
    try {
      const res = await apiFetch(apiURL('/api/notifications/test'), {
        method: 'POST',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider })
      })
      if (!res.ok) {
        const payload = await res.json().catch(() => ({}))
        throw new Error(payload.error || `request failed (${res.status})`)
      }
      if (provider === 'telegram') telegramNote = `Test message sent via ${provider}.`
      else webhookNote = `Test message sent via ${provider}.`
    } catch (e) {
      if (provider === 'telegram') telegramError = e instanceof Error ? e.message : 'Test failed'
      else webhookError = e instanceof Error ? e.message : 'Test failed'
    } finally {
      testing = ''
    }
  }

  function eventLabel(type: string): string {
    return infoFor(type).label
  }

  function badgeSeverity(type: string): 'info' | 'warn' | 'crit' {
    const severity = infoFor(type).severity
    return severity === 'crit' ? 'crit' : severity === 'warn' ? 'warn' : 'info'
  }
</script>

<section class="notifications">
  <header class="page-head">
    <div>
      <p class="eyebrow">Administration</p>
      <h1>Notifications</h1>
      <p class="muted">Review what happened and choose which events reach you, and how.</p>
    </div>
    <div class="head-actions">
      <button class="ghost" on:click={loadHistory} disabled={refreshing}>{refreshing ? 'Refreshing…' : 'Refresh'}</button>
    </div>
  </header>

  <div class="tabs" role="tablist" aria-label="Notification sections">
    <button class:active={tab === 'inbox'} on:click={() => (tab = 'inbox')}>
      Inbox {#if unreadCount > 0}<span class="count">{unreadCount}</span>{/if}
    </button>
    <button class:active={tab === 'preferences'} on:click={() => (tab = 'preferences')}>Preferences</button>
  </div>

  {#if tab === 'inbox'}
    {#if error}
      <div class="banner" role="alert">{error}<button class="inline" on:click={loadHistory}>Retry</button></div>
    {/if}

    <div class="filters">
      <div class="chips" role="group" aria-label="Filter by event">
        <button class:active={filterType === 'all'} on:click={() => (filterType = 'all')}>All</button>
        {#each catalog as entry (entry.type)}
          <button class:active={filterType === entry.type} on:click={() => (filterType = entry.type)}>{entry.label}</button>
        {/each}
      </div>
      <label class="device-filter">
        <span>Device</span>
        <select bind:value={filterDevice}>
          <option value="all">All devices</option>
          {#each devices as device (device.id)}
            <option value={device.id}>{deviceLabel(device)}</option>
          {/each}
        </select>
      </label>
      <label class="unread-filter">
        <input type="checkbox" bind:checked={unreadOnly} />
        Unread only
      </label>
      {#if unreadCount > 0}
        <button class="ghost sm" on:click={markAllRead}>Mark all read</button>
      {/if}
    </div>

    {#if loading}
      <p class="muted pad">Loading notifications…</p>
    {:else if visible.length === 0}
      <EmptyState
        icon="🔔"
        title={merged.length === 0 ? 'No notifications yet' : 'Nothing to show'}
        message={merged.length === 0
          ? 'Device, sync and enrollment events will appear here.'
          : 'No notification matches these filters.'}
      />
    {:else}
      <div class="history">
        {#each groups as group (group.label)}
          <div class="day-group">
            <div class="day-label">{group.label}</div>
            <ul class="list">
              {#each group.events as event (event.id)}
                <li class:unread={!event.read} class:read={event.read}>
                  <span class="event-icon">{iconFor(event.type)}</span>
                  <div class="event-main">
                    <div class="event-top">
                      <button class="device-link" on:click={() => onSelect(event.device_id)}>{event.hostname || event.device_id}</button>
                      <SeverityBadge severity={badgeSeverity(event.type)} />
                      <span class="event-type">{eventLabel(event.type)}</span>
                      <span class="event-time" title={event.created_at}>{formatRelative(event.created_at)}</span>
                    </div>
                    <p class="event-message">{event.message}</p>
                  </div>
                  {#if !event.read}
                    <button class="mark-read" on:click={() => markRead(event)}>Mark read</button>
                  {:else}
                    <span class="read-label">Read</span>
                  {/if}
                </li>
              {/each}
            </ul>
          </div>
        {/each}
      </div>
    {/if}
  {:else}
    {#if prefsNote}<p class="ok-inline">{prefsNote}</p>{/if}
    {#if prefsError}<p class="error-inline" role="alert">{prefsError}</p>{/if}

    <section class="card">
      <header class="card-head">
        <div>
          <h2>In-app alerts</h2>
          <p class="muted">Show a toast and play a sound while the dashboard is open.</p>
        </div>
        <label class="toggle">
          <input type="checkbox" bind:checked={form.enabled} on:change={() => (formDirty = true)} />
          <span class="track"></span>
          <span class="toggle-label">{form.enabled ? 'On' : 'Off'}</span>
        </label>
      </header>

      <div class="card-body">
        <div class="field">
          <span class="field-label">Events that pop up</span>
          <div class="chips">
            {#each catalog as entry (entry.type)}
              <button class:active={form.popups.includes(entry.type)} on:click={() => togglePopup(entry.type)}>
                {entry.label}
              </button>
            {/each}
          </div>
          <small class="muted">
            Only events that just happened pop up, and only while the dashboard is open.
            Everything is recorded in the inbox either way: a device that comes back
            online is history, not an interruption.
          </small>
        </div>

        <div class="sound-row">
          <div class="field">
            <span class="field-label">Sound</span>
            <div class="chips">
              {#each NOTIFICATION_SOUNDS as option (option.id)}
                <button class:active={form.sound === option.id} on:click={() => { formDirty = true; form = { ...form, sound: option.id } }}>{option.label}</button>
              {/each}
              <button class="ghost sm" on:click={testSound} disabled={form.sound === 'none'}>Test</button>
            </div>
          </div>
          <label class="volume">
            <span class="field-label">Volume</span>
            <input type="range" min="0" max="1" step="0.05" bind:value={form.volume} on:input={() => (formDirty = true)} />
            <span class="volume-value">{Math.round(form.volume * 100)}%</span>
          </label>
        </div>

        <div class="actions">
          <button class="primary" on:click={savePrefs} disabled={savingPrefs}>{savingPrefs ? 'Saving…' : 'Save in-app preferences'}</button>
        </div>
      </div>
    </section>

    <section class="card">
      <header class="card-head">
        <div>
          <h2>Telegram</h2>
          <p class="muted">Deliver selected events to a Telegram chat through a bot.</p>
        </div>
        <label class="toggle">
          <input type="checkbox" bind:checked={telegramEnabled} />
          <span class="track"></span>
          <span class="toggle-label">{telegramEnabled ? 'On' : 'Off'}</span>
        </label>
      </header>
      <div class="card-body">
        {#if telegramError}<p class="error-inline" role="alert">{telegramError}</p>{/if}
        {#if telegramNote}<p class="ok-inline">{telegramNote}</p>{/if}
        <div class="form-grid">
          <label>Bot token
            <input type="password" bind:value={telegramToken} placeholder="123456:ABC-DEF…" autocomplete="off" />
          </label>
          <label>Chat ID
            <div class="input-with-btn">
              <input type="text" bind:value={telegramChatId} placeholder="9999 or @channel" />
              <button class="secondary sm" on:click={detectChatId} disabled={detecting}>{detecting ? '…' : 'Detect'}</button>
            </div>
          </label>
        </div>
        <div class="field">
          <span class="field-label">Events</span>
          <div class="chips">
            {#each catalog as entry (entry.type)}
              <button class:active={telegramEvents.includes(entry.type)} on:click={() => (telegramEvents = toggleEvent(telegramEvents, entry.type))}>{entry.label}</button>
            {/each}
          </div>
        </div>
        <div class="actions">
          <button class="primary" on:click={saveTelegram} disabled={savingProvider === 'telegram'}>{savingProvider === 'telegram' ? 'Saving…' : 'Save Telegram'}</button>
          <button class="ghost" on:click={() => testProvider('telegram')} disabled={testing === 'telegram'}>{testing === 'telegram' ? 'Sending…' : 'Send test'}</button>
        </div>
      </div>
    </section>

    <section class="card">
      <header class="card-head">
        <div>
          <h2>Webhook</h2>
          <p class="muted">POST the selected events as JSON to an HTTP(S) endpoint.</p>
        </div>
        <label class="toggle">
          <input type="checkbox" bind:checked={webhookEnabled} />
          <span class="track"></span>
          <span class="toggle-label">{webhookEnabled ? 'On' : 'Off'}</span>
        </label>
      </header>
      <div class="card-body">
        {#if webhookError}<p class="error-inline" role="alert">{webhookError}</p>{/if}
        {#if webhookNote}<p class="ok-inline">{webhookNote}</p>{/if}
        <div class="form-grid">
          <label>URL
            <input type="url" bind:value={webhookUrl} placeholder="https://example.test/hook" />
          </label>
          <label>Secret (optional)
            <input type="password" bind:value={webhookSecret} placeholder="HMAC-SHA256 signing secret" autocomplete="off" />
          </label>
        </div>
        <div class="field">
          <span class="field-label">Events</span>
          <div class="chips">
            {#each catalog as entry (entry.type)}
              <button class:active={webhookEvents.includes(entry.type)} on:click={() => (webhookEvents = toggleEvent(webhookEvents, entry.type))}>{entry.label}</button>
            {/each}
          </div>
        </div>
        <div class="actions">
          <button class="primary" on:click={saveWebhook} disabled={savingProvider === 'webhook'}>{savingProvider === 'webhook' ? 'Saving…' : 'Save webhook'}</button>
          <button class="ghost" on:click={() => testProvider('webhook')} disabled={testing === 'webhook'}>{testing === 'webhook' ? 'Sending…' : 'Send test'}</button>
        </div>
      </div>
    </section>
  {/if}
</section>

<style>
  .notifications { display: grid; gap: 0.9rem; max-width: 1100px; }
  .page-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 1rem; }
  .page-head h1 { margin: 0.1rem 0 0.25rem; font-size: 1.25rem; color: var(--text-bright); }
  .eyebrow { margin: 0; color: var(--text-faint); font-size: 10px; font-weight: 700; letter-spacing: 0.09em; text-transform: uppercase; }
  .muted { color: var(--text-muted); font-size: 0.82rem; margin: 0; }
  .head-actions { display: flex; align-items: center; gap: 0.4rem; flex-shrink: 0; }

  .tabs { display: flex; gap: 0.3rem; border-bottom: 1px solid var(--border); }
  .tabs button { padding: 0.5rem 0.7rem; border: 0; border-bottom: 2px solid transparent; background: transparent; color: var(--text-faint); font: inherit; font-size: 0.8rem; font-weight: 600; cursor: pointer; }
  .tabs button:hover { color: var(--text); }
  .tabs button.active { color: var(--accent); border-bottom-color: var(--accent); }
  .count { display: inline-flex; min-width: 1.2rem; justify-content: center; margin-left: 0.3rem; padding: 0 0.3rem; border-radius: 999px; background: var(--accent-dim); color: var(--accent); font-size: 0.65rem; }

  .filters { display: flex; flex-wrap: wrap; align-items: center; gap: 0.6rem; }
  .chips { display: flex; flex-wrap: wrap; gap: 0.3rem; }
  .chips button { padding: 0.25rem 0.55rem; border: 1px solid var(--border); border-radius: 999px; background: transparent; color: var(--text-muted); font: inherit; font-size: 0.7rem; cursor: pointer; }
  .chips button:hover { border-color: var(--accent-border); color: var(--text); }
  .chips button.active { border-color: var(--accent-border); background: var(--accent-dim); color: var(--accent); }
  .device-filter { display: flex; align-items: center; gap: 0.35rem; color: var(--text-faint); font-size: 0.72rem; }
  .device-filter select { padding: 0.3rem 0.5rem; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--card); color: var(--text); font: inherit; font-size: 0.75rem; }
  .unread-filter { display: flex; align-items: center; gap: 0.35rem; color: var(--text-faint); font-size: 0.72rem; }

  .banner { display: flex; align-items: center; gap: 0.6rem; padding: 0.55rem 0.7rem; border: 1px solid rgba(248, 113, 113, 0.35); border-radius: var(--radius-sm); color: var(--crit); font-size: 0.78rem; }
  .pad { padding: 0.8rem 0; }

  .list { list-style: none; margin: 0; padding: 0; border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; background: var(--card); }
  .list li { display: flex; align-items: flex-start; gap: 0.55rem; padding: 0.7rem 0.8rem; border-bottom: 1px solid var(--border); }
  .list li:last-child { border-bottom: 0; }
  .list li.read { opacity: 0.65; }

  /* The history is read by day: one group per day, each with its own frame. */
  .day-group + .day-group { margin-top: 0.8rem; }
  .day-label {
    padding: 0.35rem 0.8rem;
    color: var(--text-faint);
    font-size: 0.66rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.07em;
  }
  .event-icon { font-size: 0.95rem; line-height: 1.4; }
  .event-main { min-width: 0; flex: 1; }
  .event-top { display: flex; align-items: center; flex-wrap: wrap; gap: 0.4rem; }
  .device-link { padding: 0; border: 0; background: transparent; color: var(--text); font: inherit; font-size: 0.78rem; font-weight: 600; cursor: pointer; }
  .device-link:hover { color: var(--accent); }
  .event-type { color: var(--text-faint); font-size: 0.7rem; }
  .event-time { margin-left: auto; color: var(--text-faint); font-size: 0.68rem; white-space: nowrap; }
  .event-message { margin: 0.2rem 0 0; color: var(--text-muted); font-size: 0.78rem; overflow-wrap: anywhere; }
  .mark-read { flex-shrink: 0; padding: 0.25rem 0.5rem; border: 1px solid var(--border-strong); border-radius: var(--radius-sm); background: transparent; color: var(--text-muted); font: inherit; font-size: 0.68rem; cursor: pointer; }
  .mark-read:hover { border-color: var(--accent-border); color: var(--accent); }
  .read-label { flex-shrink: 0; color: var(--text-faint); font-size: 0.68rem; }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); overflow: hidden; }
  .card-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 0.75rem; padding: 0.75rem 0.9rem; border-bottom: 1px solid var(--border); }
  .card-head h2 { margin: 0 0 0.15rem; font-size: 0.88rem; color: var(--text); }
  .card-body { display: grid; gap: 0.8rem; padding: 0.85rem 0.9rem; }
  .field { display: grid; gap: 0.3rem; }
  .field-label { color: var(--text-faint); font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.04em; }
  .form-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 0.6rem; }
  .form-grid label { display: grid; gap: 0.25rem; color: var(--text-faint); font-size: 0.72rem; }
  .form-grid input { padding: 0.4rem 0.5rem; border: 1px solid var(--border); border-radius: var(--radius-sm); background: var(--bg); color: var(--text); font: inherit; font-size: 0.78rem; }
  .input-with-btn { display: flex; gap: 0.35rem; }
  .input-with-btn input { flex: 1; }
  .sound-row { display: flex; flex-wrap: wrap; align-items: flex-end; gap: 1rem; }
  .volume { display: flex; align-items: center; gap: 0.45rem; color: var(--text-faint); font-size: 0.72rem; }
  .volume input { width: 120px; accent-color: var(--accent); }
  .volume-value { color: var(--text-muted); font-variant-numeric: tabular-nums; }
  .actions { display: flex; gap: 0.45rem; flex-wrap: wrap; }
  .toggle { display: flex; align-items: center; gap: 0.4rem; cursor: pointer; }
  .toggle input { position: absolute; opacity: 0; pointer-events: none; }
  .track { width: 30px; height: 17px; border-radius: 999px; background: var(--border-strong); position: relative; transition: background 0.15s ease; }
  .track::after { content: ''; position: absolute; top: 2px; left: 2px; width: 13px; height: 13px; border-radius: 50%; background: var(--text); transition: transform 0.15s ease; }
  .toggle input:checked + .track { background: var(--accent); }
  .toggle input:checked + .track::after { transform: translateX(13px); background: var(--bg); }
  .toggle-label { color: var(--text-muted); font-size: 0.7rem; }
  .ok-inline { margin: 0; color: var(--ok); font-size: 0.78rem; }
  .error-inline { margin: 0; color: var(--crit); font-size: 0.78rem; }

  @media (max-width: 800px) {
    .event-time { margin-left: 0; }
    .list li { flex-wrap: wrap; }
    .mark-read, .read-label { margin-left: 1.6rem; }
  }
</style>
