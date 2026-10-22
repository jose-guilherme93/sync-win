import { describe, expect, it } from 'vitest'
import {
  DEFAULT_POPUPS,
  FRESH_WINDOW_MS,
  isFreshEvent,
  shouldNotify,
  shouldToast,
  type NotificationPrefs
} from './notification-prefs'

const NOW = Date.parse('2026-10-22T12:00:00Z')

function prefs(over: Partial<NotificationPrefs> = {}): NotificationPrefs {
  return {
    enabled: true,
    events: ['device_offline', 'device_online', 'sync_error', 'device_enrolled'],
    popups: [...DEFAULT_POPUPS],
    sound: 'beep',
    volume: 0.5,
    ...over
  }
}

function event(over: Partial<{ type: string; created_at: string }> = {}) {
  return {
    type: over.type ?? 'device_offline',
    created_at: over.created_at ?? new Date(NOW).toISOString()
  }
}

describe('isFreshEvent', () => {
  it('accepts an event that just happened', () => {
    expect(isFreshEvent(event({ created_at: new Date(NOW - 1000).toISOString() }), NOW)).toBe(true)
  })

  it('rejects an event from the past', () => {
    // This is the rule that stops the dashboard from replaying its history as
    // toasts every time it is opened.
    const old = new Date(NOW - 5 * 60_000).toISOString()
    expect(isFreshEvent(event({ created_at: old }), NOW)).toBe(false)
  })

  it('rejects an event with no usable timestamp', () => {
    expect(isFreshEvent({ created_at: '' }, NOW)).toBe(false)
    expect(isFreshEvent({ created_at: '0001-01-01T00:00:00Z' }, NOW)).toBe(false)
    expect(isFreshEvent({}, NOW)).toBe(false)
  })

  it('tolerates a clock that runs ahead of the server', () => {
    const ahead = new Date(NOW + 5000).toISOString()
    expect(isFreshEvent(event({ created_at: ahead }), NOW)).toBe(true)
  })

  it('rejects an event that is outside the window on either side', () => {
    const far = new Date(NOW - FRESH_WINDOW_MS - 1000).toISOString()
    expect(isFreshEvent(event({ created_at: far }), NOW)).toBe(false)
  })
})

describe('shouldToast', () => {
  it('toasts an event that needs attention and just happened', () => {
    expect(shouldToast(prefs(), event({ type: 'device_offline' }), NOW)).toBe(true)
    expect(shouldToast(prefs(), event({ type: 'sync_error' }), NOW)).toBe(true)
  })

  it('does not toast a device that came back online', () => {
    // The event is recorded in the inbox; it must not interrupt.
    expect(shouldToast(prefs(), event({ type: 'device_online' }), NOW)).toBe(false)
  })

  it('does not toast an enrollment', () => {
    expect(shouldToast(prefs(), event({ type: 'device_enrolled' }), NOW)).toBe(false)
  })

  it('does not toast when the in-app alerts are off', () => {
    expect(shouldToast(prefs({ enabled: false }), event(), NOW)).toBe(false)
  })

  it('toasts an event the person asked for', () => {
    const custom = prefs({ popups: ['device_online'] })
    expect(shouldToast(custom, event({ type: 'device_online' }), NOW)).toBe(true)
  })

  it('does not toast an old event even when the type is subscribed', () => {
    const old = new Date(NOW - 60_000).toISOString()
    expect(shouldToast(prefs(), event({ created_at: old }), NOW)).toBe(false)
  })
})

describe('shouldNotify', () => {
  it('follows the subscription list', () => {
    expect(shouldNotify(prefs(), 'device_offline')).toBe(true)
    expect(shouldNotify(prefs({ events: ['sync_error'] }), 'device_offline')).toBe(false)
    expect(shouldNotify(prefs({ enabled: false }), 'device_offline')).toBe(false)
  })
})
