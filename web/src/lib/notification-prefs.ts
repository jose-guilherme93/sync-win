import { writable } from 'svelte/store'
import { apiFetch, apiURL } from './api'
import type { NotificationSound } from './notification-sound'

// The in-app notification preferences live on the server (the existing
// web_inbox provider config), so they follow the account across browsers.
// localStorage was only used for the sound and was never updated when the
// settings modal saved, which is why changing the sound appeared to do nothing.
export type NotificationPrefs = {
  enabled: boolean
  events: string[]
  sound: NotificationSound
  volume: number
}

export const DEFAULT_EVENTS = ['device_offline', 'device_online', 'sync_error', 'device_enrolled']

export const DEFAULT_NOTIFICATION_PREFS: NotificationPrefs = {
  enabled: true,
  events: [...DEFAULT_EVENTS],
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
  }
}

export function shouldNotify(prefs: NotificationPrefs, type: string): boolean {
  return prefs.enabled && prefs.events.includes(type)
}

export function resetNotificationPrefs(): void {
  notificationPrefs.set({ ...DEFAULT_NOTIFICATION_PREFS, events: [...DEFAULT_EVENTS] })
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

  return {
    enabled: inbox.enabled !== false,
    events,
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
        events_set: true
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
    events: [...prefs.events]
  })
}
