import { describe, expect, it } from 'vitest'
import {
  MAX_TERMINAL_TABS,
  addTerminalTab,
  canOpenTerminal,
  closeTerminalTab,
  nextActiveTerminal,
  terminalAccountLabel,
  type TerminalTab
} from './terminal-tabs'

function tab(id: string, label = '', status: TerminalTab['status'] = 'connecting'): TerminalTab {
  return { id, user: label, label, title: '', status, error: '' }
}

describe('terminal tabs', () => {
  it('allows new terminals until the six-session limit is reached', () => {
    expect(canOpenTerminal([])).toBe(true)
    expect(canOpenTerminal([tab('a'), tab('b'), tab('c'), tab('d'), tab('e')])).toBe(true)
    expect(canOpenTerminal([tab('a'), tab('b'), tab('c'), tab('d'), tab('e'), tab('f')])).toBe(false)
    expect(MAX_TERMINAL_TABS).toBe(6)
  })

  it('refuses a terminal beyond the limit without dropping the existing ones', () => {
    const tabs = [tab('a'), tab('b'), tab('c'), tab('d'), tab('e'), tab('f')]
    const added = addTerminalTab(tabs, tab('g'))

    expect(added).toHaveLength(6)
    expect(added.map((entry) => entry.id)).toEqual(['a', 'b', 'c', 'd', 'e', 'f'])
  })

  it('appends a terminal while there is room', () => {
    const added = addTerminalTab([tab('a')], tab('b', 'alice'))

    expect(added.map((entry) => entry.id)).toEqual(['a', 'b'])
    expect(added[1].label).toBe('alice')
  })

  it('uses the account as the fallback label without numbering', () => {
    expect(terminalAccountLabel('')).toBe('default')
    expect(terminalAccountLabel('  ')).toBe('default')
    expect(terminalAccountLabel(' alice ')).toBe('alice')
    // Two shells for the same account share the fallback; the live program
    // title is what tells the tabs apart.
    expect(terminalAccountLabel('alice')).toBe('alice')
  })

  it('keeps a label stable when a sibling closes', () => {
    const remaining = closeTerminalTab([tab('a', 'alice'), tab('b', 'alice')], 'a')

    expect(remaining[0].label).toBe('alice')
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
