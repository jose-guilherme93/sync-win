// Dashboard panels can be reordered, resized to half/full width and hidden,
// one screen at a time. The layout is a browser preference, not device state:
// it lives in localStorage, survives reloads and is never sent to the server.
//
// The rules live here rather than in the component so that a corrupt or
// outdated stored layout cannot blank a screen: a panel that no longer exists
// is dropped, a new panel is inserted, and an unreadable value falls back to
// the defaults.

export type PanelSpan = 6 | 12

export type PanelState = {
  id: string
  span: PanelSpan
  hidden: boolean
}

export type PanelDefinition = {
  id: string
  label: string
  defaultSpan: PanelSpan
  defaultHidden?: boolean
}

export type DashboardLayout = {
  version: 1
  panels: PanelState[]
}

export const LAYOUT_VERSION = 1 as const

export function defaultPanels(definitions: PanelDefinition[]): PanelState[] {
  return definitions.map((definition) => ({
    id: definition.id,
    span: definition.defaultSpan,
    hidden: definition.defaultHidden === true
  }))
}

// normalizePanels makes any stored value safe to render. Unknown ids are
// dropped, duplicates keep the first position, and definitions that did not
// exist when the layout was saved are appended in their declared order.
export function normalizePanels(value: unknown, definitions: PanelDefinition[]): PanelState[] {
  const byId = new Map(definitions.map((definition) => [definition.id, definition]))
  const fallback = defaultPanels(definitions)
  const raw = Array.isArray(value)
    ? value
    : value && typeof value === 'object' && Array.isArray((value as { panels?: unknown }).panels)
      ? (value as { panels: unknown[] }).panels
      : null
  if (!raw) return fallback

  const seen = new Set<string>()
  const panels: PanelState[] = []
  for (const entry of raw) {
    if (!entry || typeof entry !== 'object') continue
    const candidate = entry as { id?: unknown; span?: unknown; hidden?: unknown }
    if (typeof candidate.id !== 'string' || seen.has(candidate.id)) continue
    const definition = byId.get(candidate.id)
    if (!definition) continue
    seen.add(candidate.id)
    const span: PanelSpan = candidate.span === 6 || candidate.span === 12
      ? candidate.span
      : definition.defaultSpan
    panels.push({
      id: candidate.id,
      span,
      hidden: candidate.hidden === true
    })
  }
  // An entirely unreadable layout must not leave the screen empty: fall back
  // rather than honour a value that kept nothing.
  if (panels.length === 0) return fallback
  for (const definition of definitions) {
    if (!seen.has(definition.id)) {
      panels.push({ id: definition.id, span: definition.defaultSpan, hidden: definition.defaultHidden === true })
    }
  }
  return panels
}

export function parsePanels(raw: string | null, definitions: PanelDefinition[]): PanelState[] {
  if (!raw) return defaultPanels(definitions)
  try {
    return normalizePanels(JSON.parse(raw), definitions)
  } catch {
    return defaultPanels(definitions)
  }
}

export function panelStorageKey(screen: string): string {
  let owner = ''
  try {
    owner = localStorage.getItem('sync-win-owner-id') || ''
  } catch {
    owner = ''
  }
  const scope = owner ? owner.replace(/[^a-zA-Z0-9._-]/g, '').slice(0, 48) : 'anonymous'
  return `sync-win-layout-${scope}-${screen}`
}

export function loadPanels(key: string, definitions: PanelDefinition[]): PanelState[] {
  try {
    return parsePanels(localStorage.getItem(key), definitions)
  } catch {
    return defaultPanels(definitions)
  }
}

export function savePanels(key: string, panels: PanelState[]): boolean {
  try {
    const layout: DashboardLayout = { version: LAYOUT_VERSION, panels }
    localStorage.setItem(key, JSON.stringify(layout))
    return true
  } catch {
    // A blocked or full localStorage must not break the drag operation in
    // progress; the layout still applies for this session.
    return false
  }
}

// reorderPanels moves `draggedId` to the position of `targetId`, taking it out
// of its old place first. Dropping a panel onto itself is a no-op.
export function reorderPanels(panels: PanelState[], draggedId: string, targetId: string): PanelState[] {
  if (!draggedId || !targetId || draggedId === targetId) return panels
  const from = panels.findIndex((panel) => panel.id === draggedId)
  if (from < 0) return panels
  const moving = panels[from]
  const without = panels.filter((panel) => panel.id !== draggedId)
  const target = without.findIndex((panel) => panel.id === targetId)
  if (target < 0) return panels
  return [...without.slice(0, target), moving, ...without.slice(target)]
}

export function movePanelTo(panels: PanelState[], id: string, targetIndex: number): PanelState[] {
  const from = panels.findIndex((panel) => panel.id === id)
  if (from < 0) return panels
  const clamped = Math.max(0, Math.min(panels.length - 1, targetIndex))
  if (clamped === from) return panels
  const next = panels.slice()
  const [moving] = next.splice(from, 1)
  next.splice(clamped, 0, moving)
  return next
}

// movePanelStep moves a panel by one visible neighbour. Hidden panels are
// skipped so the arrow buttons always produce a visible change.
export function movePanelStep(panels: PanelState[], id: string, delta: number): PanelState[] {
  const visible = panels.filter((panel) => !panel.hidden)
  const index = visible.findIndex((panel) => panel.id === id)
  const target = visible[index + delta]
  if (index < 0 || !target) return panels
  // Swap the two positions in the full list, not "insert before". The latter
  // would jump a panel past its neighbour when a hidden panel sits between
  // them, and the arrow would look like it moved two places.
  const from = panels.findIndex((panel) => panel.id === id)
  const to = panels.findIndex((panel) => panel.id === target.id)
  const next = panels.slice()
  ;[next[from], next[to]] = [next[to], next[from]]
  return next
}

export function setPanelSpan(panels: PanelState[], id: string, span: PanelSpan): PanelState[] {
  return panels.map((panel) => (panel.id === id ? { ...panel, span } : panel))
}

export function togglePanelSpan(panels: PanelState[], id: string): PanelState[] {
  const panel = panels.find((item) => item.id === id)
  if (!panel) return panels
  return setPanelSpan(panels, id, panel.span === 12 ? 6 : 12)
}

export function setPanelHidden(panels: PanelState[], id: string, hidden: boolean): PanelState[] {
  return panels.map((panel) => (panel.id === id ? { ...panel, hidden } : panel))
}

// labelFor resolves a panel id to its human name. Unknown ids fall back to the
// id so a stale layout cannot render a blank control.
export function labelFor(definitions: PanelDefinition[], id: string): string {
  return definitions.find((definition) => definition.id === id)?.label ?? id
}
