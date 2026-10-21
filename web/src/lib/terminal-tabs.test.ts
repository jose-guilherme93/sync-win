import { describe, expect, it } from 'vitest'
import {
  MAX_TERMINAL_TABS,
  addTerminalTab,
  canOpenTerminal,
  closeTerminalTab,
  nextActiveTerminal,
  nextTerminalLabel,
  type TerminalTab
} from './terminal-tabs'

function tab(id: string, label = '', user = '', status: TerminalTab['status'] = 'connecting'): TerminalTab {
  return { id, user: user || label, label, status, error: '' }
}

describe('terminal tabs', () => {
  it('allows new terminals until the per-device limit is reached', () => {
    expect(canOpenTerminal([])).toBe(true)
    expect(canOpenTerminal([tab('a'), tab('b')])).toBe(true)
    expect(canOpenTerminal([tab('a'), tab('b'), tab('c')])).toBe(false)
    expect(MAX_TERMINAL_TABS).toBe(3)
  })

  it('refuses a terminal beyond the limit without dropping the existing ones', () => {
    const tabs = [tab('a'), tab('b'), tab('c')]
    const added = addTerminalTab(tabs, tab('d'))

    expect(added).toHaveLength(3)
    expect(added.map((entry) => entry.id)).toEqual(['a', 'b', 'c'])
  })

  it('appends a terminal while there is room', () => {
    const added = addTerminalTab([tab('a')], tab('b', 'alice'))

    expect(added.map((entry) => entry.id)).toEqual(['a', 'b'])
    expect(added[1].label).toBe('alice')
  })

  it('labels tabs by account and counts repeated accounts', () => {
    expect(nextTerminalLabel([], 'alice')).toBe('alice')
    expect(nextTerminalLabel([tab('a', 'alice')], 'alice')).toBe('alice (2)')
    expect(nextTerminalLabel([tab('a', 'alice'), tab('b', 'alice (2)')], 'alice')).toBe('alice (3)')
    expect(nextTerminalLabel([tab('a', 'alice')], 'bob')).toBe('bob')
  })

  it('keeps a label stable when a sibling closes', () => {
    const tabs = [tab('a', 'alice', 'alice'), tab('b', 'alice (2)', 'alice')]
    const remaining = closeTerminalTab(tabs, 'a')

    expect(remaining[0].label).toBe('alice (2)')
  })

  it('falls back to the device default account label and trims whitespace', () => {
    expect(nextTerminalLabel([], '  ')).toBe('default')
    expect(nextTerminalLabel([], ' alice ')).toBe('alice')
  })

  it('removes only the closed terminal', () => {
    const remaining = closeTerminalTab([tab('a'), tab('b'), tab('c')], 'b')

    expect(remaining.map((entry) => entry.id)).toEqual(['a', 'c'])
  })

  it('activates the neighbouring tab after the active one closes', () => {
    const tabs = [tab('a'), tab('b'), tab('c')]

    expect(nextActiveTerminal(tabs, 'a')).toBe('b')
    expect(nextActiveTerminal(tabs, 'b')).toBe('c')
    expect(nextActiveTerminal(tabs, 'c')).toBe('b')
  })

  it('leaves no active tab when the last one closes', () => {
    expect(nextActiveTerminal([tab('a')], 'a')).toBe('')
  })

  it('keeps the first remaining tab active when the closing id is unknown', () => {
    expect(nextActiveTerminal([tab('a'), tab('b')], 'missing')).toBe('a')
  })
})
