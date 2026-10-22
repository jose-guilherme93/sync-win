import { describe, expect, it } from 'vitest'
import { eventDetail, eventIcon } from './notification-format'

describe('eventDetail', () => {
  it('drops the icon and the device name the dashboard already shows', () => {
    expect(eventDetail('🔴 [alpine-test-1] went offline')).toBe('went offline')
    expect(eventDetail('➕ [alpine-test-1] was enrolled')).toBe('was enrolled')
    expect(eventDetail('⚠️ [pc] timeout')).toBe('timeout')
  })

  it('keeps a message that has no prefix', () => {
    expect(eventDetail('something went wrong')).toBe('something went wrong')
  })

  it('keeps a message that is only a prefix', () => {
    expect(eventDetail('🔴 [pc]')).toBe('🔴 [pc]')
  })

  it('survives an empty message', () => {
    expect(eventDetail('')).toBe('')
  })
})

describe('eventIcon', () => {
  it('maps the event types the server emits', () => {
    expect(eventIcon('device_offline')).toBe('🔴')
    expect(eventIcon('device_online')).toBe('🟢')
    expect(eventIcon('sync_error')).toBe('⚠️')
    expect(eventIcon('device_enrolled')).toBe('➕')
    expect(eventIcon('something_new')).toBe('ℹ️')
  })
})
