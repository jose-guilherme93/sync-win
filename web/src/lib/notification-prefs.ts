import { writable } from 'svelte/store'
import { apiFetch, apiURL } from './api'
import { parseTimestamp } from './format'
import type { NotificationSound } from './notification-sound'

// The in-app notification preferences live on the server (the existing
// web_inbox provider config), so they follow the account across browsers.
// localStorage was only used for the sound and was never updated when the
// settings modal saved, which is why changing the sound appeared to do nothing.
export type NotificationPrefs = {
  enabled: boolean
  // events is what the account is subscribed to. For the external providers it
  // decides what is delivered; the in-app channel records everything.
  events: string[]
  // popups is the narrower list of events that raise a toast. Everything is
  // recorded in the inbox; only these interrupt.
  popups: string[]
  sound: NotificationSound
  volume: number
}

export const DEFAULT_EVENTS = ['device_offline', 'device_online', 'sync_error', 'device_enrolled']

// The fallback for the popup list, used until the server answers and if the
// request fails. It mirrors the events the server's catalog marks as needing
// attention; the server sends the effective list in the same payload, so this
// is only what the dashboard starts with.
export const DEFAULT_POPUPS = ['device_offline', 'sync_error']

// FRESH_WINDOW_MS is how long an event may be old and still raise a toast.
//
// A toast is for something that is happening now. An event that the dashboard
// only learns about when it opens — a device that came back an hour ago, the
// history of a night — belongs in the inbox, not on top of the screen. The
// window also absorbs the clock skew between the server and the browser.
export const FRESH_WINDOW_MS = 60_000

export const DEFAULT_NOTIFICATION_PREFS: NotificationPrefs = {
  enabled: true,
  events: [...DEFAULT_EVENTS],
  popups: [...DEFAULT_POPUPS],
  sound: 'beep',
  volume: 0.5
}

export const notificationPrefs = writable<NotificationPrefs>({ ...DEFAULT_NOTIFICATION_PREFS })

const SOUNDS: NotificationSound[] = ['beep', 'chime', 'pulse', 'none']

type ConfigResponse = {
  provider: string
  enabled: boolean
  events?: string[]
  configured?: boolean
  config?: {
    sound?: string
    volume?: number
    events_set?: boolean
    popups?: string[]
    popups_set?: boolean
  }
}

// shouldNotify reports whether the account is subscribed to an event.
export function shouldNotify(prefs: NotificationPrefs, type: string): boolean {
  return prefs.enabled && prefs.events.includes(type)
}

// isFreshEvent reports whether an event is recent enough to interrupt. A
// missing or unparsable timestamp is not fresh: an event nobody can date is
// not something to pop up over the dashboard.
export function isFreshEvent(event: { created_at?: string | null }, now: number = Date.now()): boolean {
  const stamp = parseTimestamp(event.created_at)
  if (stamp == null) return false
  const age = now - stamp
  // A small negative age means the server's clock is ahead of the browser's.
  return age < FRESH_WINDOW_MS && age > -FRESH_WINDOW_MS
}

// shouldToast is the one decision that decides what pops up over the
// dashboard: the in-app alerts are on, the event is one the person chose to
// be interrupted by, and it just happened.
export function shouldToast(
  prefs: NotificationPrefs,
  event: { type: string; created_at?: string | null },
  now: number = Date.now()
): boolean {
  if (!prefs.enabled) return false
  if (!prefs.popups.includes(event.type)) return false
  return isFreshEvent(event, now)
}

export function resetNotificationPrefs(): void {
  notificationPrefs.set({
    ...DEFAULT_NOTIFICATION_PREFS,
    events: [...DEFAULT_EVENTS],
    popups: [...DEFAULT_POPUPS]
  })
}

function normalize(inbox: ConfigResponse): NotificationPrefs {
  const sound = SOUNDS.includes(inbox.config?.sound as NotificationSound)
    ? (inbox.config?.sound as NotificationSound)
    : 'beep'
  const volume = typeof inbox.config?.volume === 'number'
    ? Math.min(1, Math.max(0, inbox.config.volume))
    : 0.5
  // events_set distinguishes a deliberate empty selection from a row created by
  // the old settings modal, which always stored an empty list while showing
  // every event.
  const events = inbox.config?.events_set === true && Array.isArray(inbox.events)
    ? [...inbox.events]
    : [...DEFAULT_EVENTS]

  // The server sends the effective popup list. popups_set marks a deliberate
  // empty selection, which is different from a row that never had the setting.
  const popups = inbox.config?.popups_set === true
    ? (Array.isArray(inbox.config?.popups) ? [...inbox.config!.popups!] : [])
    : (Array.isArray(inbox.config?.popups) && inbox.config!.popups!.length > 0
        ? [...inbox.config!.popups!]
        : [...DEFAULT_POPUPS])

  return {
    enabled: inbox.enabled !== false,
    events,
    popups,
    sound,
    volume
  }
}

export async function loadNotificationPrefs(authHeaders: Record<string, string>): Promise<void> {
  try {
    const res = await apiFetch(apiURL('/api/notifications/config'), { headers: authHeaders })
    if (!res.ok) return
    const configs: ConfigResponse[] = await res.json()
    const inbox = Array.isArray(configs) ? configs.find((cfg) => cfg.provider === 'web_inbox') : undefined
    if (!inbox) {
      resetNotificationPrefs()
      return
    }
    notificationPrefs.set(normalize(inbox))
  } catch {
    // Keep the defaults; notifications are not worth an error banner.
  }
}

export async function saveInAppPrefs(prefs: NotificationPrefs, authHeaders: Record<string, string>): Promise<void> {
  const res = await apiFetch(apiURL('/api/notifications/config'), {
    method: 'PUT',
    headers: { ...authHeaders, 'Content-Type': 'application/json' },
    body: JSON.stringify({
      provider: 'web_inbox',
      enabled: prefs.enabled,
      events: prefs.events,
      config: {
        sound: prefs.sound,
        volume: Math.min(1, Math.max(0, prefs.volume)),
        events_set: true,
        popups: [...prefs.popups],
        popups_set: true
      }
    })
  })
  if (!res.ok) {
    const payload = await res.json().catch(() => ({}))
    throw new Error(payload.error || `request failed (${res.status})`)
  }
  notificationPrefs.set({
    ...prefs,
    volume: Math.min(1, Math.max(0, prefs.volume)),
    events: [...prefs.events],
    popups: [...prefs.popups]
  })
}
