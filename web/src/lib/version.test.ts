import { describe, expect, it } from 'vitest'
import { compareVersions, isAgentOutdated } from './version'

describe('compareVersions', () => {
  it('orders numeric segments, not strings', () => {
    expect(compareVersions('0.9.0', '0.11.0')).toBe(-1)
    expect(compareVersions('0.11.0', '0.9.0')).toBe(1)
    expect(compareVersions('1.0.0', '1.0.0')).toBe(0)
    expect(compareVersions('v0.11.2', '0.11.1')).toBe(1)
    expect(compareVersions('0.11', '0.11.0')).toBe(0)
  })
})

describe('isAgentOutdated', () => {
  it('flags a device behind the shipped agent', () => {
    expect(isAgentOutdated('0.9.0', '0.11.2')).toBe(true)
  })

  it('does not flag a device on the shipped version or a dev build', () => {
    expect(isAgentOutdated('0.11.2', '0.11.2')).toBe(false)
    expect(isAgentOutdated('0.12.0', '0.11.2')).toBe(false)
    expect(isAgentOutdated('dev', '0.11.2')).toBe(false)
    expect(isAgentOutdated('', '0.11.2')).toBe(false)
    expect(isAgentOutdated('0.9.0', '')).toBe(false)
  })
})
