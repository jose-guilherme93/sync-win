import { beforeEach, describe, expect, it } from 'vitest'
import {
  defaultPanels,
  labelFor,
  loadPanels,
  movePanelStep,
  normalizePanels,
  panelStorageKey,
  parsePanels,
  reorderPanels,
  savePanels,
  setPanelHidden,
  setPanelSpan,
  togglePanelSpan,
  type PanelDefinition
} from './dashboard-layout'

const DEFINITIONS: PanelDefinition[] = [
  { id: 'vitals', label: 'Vitals', defaultSpan: 12 },
  { id: 'traffic', label: 'Traffic', defaultSpan: 12 },
  { id: 'health', label: 'Packet health', defaultSpan: 6 },
  { id: 'wireless', label: 'Wireless', defaultSpan: 6 }
]

beforeEach(() => {
  localStorage.clear()
})

describe('defaultPanels', () => {
  it('follows the declared order and spans', () => {
    expect(defaultPanels(DEFINITIONS)).toEqual([
      { id: 'vitals', span: 12, hidden: false },
      { id: 'traffic', span: 12, hidden: false },
      { id: 'health', span: 6, hidden: false },
      { id: 'wireless', span: 6, hidden: false }
    ])
  })
})

describe('normalizePanels', () => {
  it('drops unknown panels and appends new ones in definition order', () => {
    const panels = normalizePanels({
      version: 1,
      panels: [
        { id: 'health', span: 12, hidden: true },
        { id: 'removed-panel', span: 6, hidden: false }
      ]
    }, DEFINITIONS)

    expect(panels.map((panel) => panel.id)).toEqual(['health', 'vitals', 'traffic', 'wireless'])
    expect(panels[0]).toEqual({ id: 'health', span: 12, hidden: true })
    expect(panels[1].span).toBe(12)
  })

  it('repairs an invalid span to the default', () => {
    const panels = normalizePanels([{ id: 'health', span: 3, hidden: 'yes' }], DEFINITIONS)
    expect(panels[0]).toEqual({ id: 'health', span: 6, hidden: false })
  })

  it('never returns an empty layout', () => {
    expect(normalizePanels({ panels: [] }, DEFINITIONS)).toEqual(defaultPanels(DEFINITIONS))
    expect(normalizePanels(null, DEFINITIONS)).toEqual(defaultPanels(DEFINITIONS))
    expect(normalizePanels({ panels: [{ id: 'nope' }] }, DEFINITIONS)).toEqual(defaultPanels(DEFINITIONS))
  })

  it('keeps the first occurrence of a duplicate', () => {
    const panels = normalizePanels([
      { id: 'health', span: 6, hidden: false },
      { id: 'health', span: 12, hidden: true }
    ], DEFINITIONS)
    expect(panels.filter((panel) => panel.id === 'health')).toHaveLength(1)
    expect(panels[0].span).toBe(6)
  })
})

describe('parse and storage', () => {
  it('falls back to the defaults on malformed JSON', () => {
    expect(parsePanels('{not json', DEFINITIONS)).toEqual(defaultPanels(DEFINITIONS))
  })

  it('round-trips through localStorage', () => {
    const key = panelStorageKey('network')
    const panels = setPanelSpan(defaultPanels(DEFINITIONS), 'health', 12)
    expect(savePanels(key, panels)).toBe(true)
    expect(loadPanels(key, DEFINITIONS)).toEqual(panels)
  })

  it('scopes the key to the signed-in owner', () => {
    expect(panelStorageKey('network')).toBe('sync-win-layout-anonymous-network')
    localStorage.setItem('sync-win-owner-id', 'owner/with spaces')
    expect(panelStorageKey('network')).toBe('sync-win-layout-ownerwithspaces-network')
  })

  it('survives storage being unavailable', () => {
    const original = window.localStorage
    const throwing = {
      getItem: () => { throw new Error('blocked') },
      setItem: () => { throw new Error('blocked') },
      removeItem: () => { throw new Error('blocked') },
      clear: () => {},
      key: () => null,
      length: 0
    } as unknown as Storage
    Object.defineProperty(window, 'localStorage', { value: throwing, configurable: true })
    expect(loadPanels('x', DEFINITIONS)).toEqual(defaultPanels(DEFINITIONS))
    expect(savePanels('x', defaultPanels(DEFINITIONS))).toBe(false)
    Object.defineProperty(window, 'localStorage', { value: original, configurable: true })
  })
})

describe('reordering', () => {
  it('moves a panel before the drop target', () => {
    const panels = defaultPanels(DEFINITIONS)
    const next = reorderPanels(panels, 'wireless', 'health')
    expect(next.map((panel) => panel.id)).toEqual(['vitals', 'traffic', 'wireless', 'health'])
  })

  it('ignores a drop on itself or an unknown id', () => {
    const panels = defaultPanels(DEFINITIONS)
    expect(reorderPanels(panels, 'health', 'health')).toBe(panels)
    expect(reorderPanels(panels, 'nope', 'health')).toBe(panels)
  })

  it('steps over hidden panels so the arrow always moves visibly', () => {
    let panels = setPanelHidden(defaultPanels(DEFINITIONS), 'traffic', true)
    panels = movePanelStep(panels, 'health', -1)
    expect(panels.filter((panel) => !panel.hidden).map((panel) => panel.id)).toEqual(['health', 'vitals', 'wireless'])
  })
})

describe('span and visibility', () => {
  it('toggles half and full width', () => {
    let panels = defaultPanels(DEFINITIONS)
    panels = togglePanelSpan(panels, 'health')
    expect(panels.find((panel) => panel.id === 'health')?.span).toBe(12)
    panels = togglePanelSpan(panels, 'health')
    expect(panels.find((panel) => panel.id === 'health')?.span).toBe(6)
  })

  it('hides and shows a panel without losing its size', () => {
    let panels = setPanelSpan(defaultPanels(DEFINITIONS), 'health', 12)
    panels = setPanelHidden(panels, 'health', true)
    expect(panels.find((panel) => panel.id === 'health')).toEqual({ id: 'health', span: 12, hidden: true })
    panels = setPanelHidden(panels, 'health', false)
    expect(panels.find((panel) => panel.id === 'health')?.hidden).toBe(false)
  })

  it('resolves a label from the definitions', () => {
    expect(labelFor(DEFINITIONS, 'health')).toBe('Packet health')
    expect(labelFor(DEFINITIONS, 'gone')).toBe('gone')
  })
})
