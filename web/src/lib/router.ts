import { writable, derived, get } from 'svelte/store'

// Navigation state for the panel shell.
//
// The whole point of the redesign is that nothing opens in a popup: selecting a
// device or a section just changes this store, and the single <main> area
// re-renders. There is one source of truth for "where am I", which also makes
// deep links and browser back/forward feasible later.

export type Section =
  // Fleet-wide
  | 'home'
  | 'devices'
  | 'alerts'
  | 'findings'
  | 'reports'
  // Device-scoped — hardware
  | 'overview'
  | 'cpu'
  | 'memory'
  | 'storage'
  | 'network'
  | 'sensors'
  // Device-scoped — system
  | 'containers'
  | 'processes'
  | 'services'
  | 'packages'
  | 'logs'
  | 'security'
  // Device-scoped — customize
  | 'remote'
  | 'settings'

export type NavState = {
  section: Section
  deviceId: string | null
}

// Sections that only make sense with a device selected. Used to decide whether
// a click in the sidebar should be allowed and whether to fall back to the
// fleet view when the selected device disappears.
export const DEVICE_SCOPED: ReadonlySet<Section> = new Set<Section>([
  'overview', 'cpu', 'memory', 'storage', 'network', 'sensors',
  'containers', 'processes', 'services', 'packages', 'logs',
  'security', 'remote', 'settings'
])

// The sidebar is data-driven: it renders these groups in order and marks the
// entry whose `section` matches the active one. Keeping the structure here
// rather than in the markup means adding a screen is a one-line change.
export type NavItem = { section: Section; label: string; icon: string; deviceScoped?: boolean }
export type NavGroup = { id: string; label: string; items: NavItem[]; deviceScoped?: boolean }

export const NAV_GROUPS: NavGroup[] = [
  {
    id: 'fleet',
    label: '',
    items: [
      { section: 'home', label: 'Home', icon: 'grid' },
      { section: 'devices', label: 'Devices', icon: 'server' },
      { section: 'alerts', label: 'Alerts', icon: 'bell' },
      { section: 'findings', label: 'Findings', icon: 'shield' },
      { section: 'reports', label: 'Reports', icon: 'chart' }
    ]
  },
  {
    id: 'hardware',
    label: 'Hardware',
    deviceScoped: true,
    items: [
      { section: 'overview', label: 'Overview', icon: 'gauge', deviceScoped: true },
      { section: 'cpu', label: 'CPU & Thermal', icon: 'cpu', deviceScoped: true },
      { section: 'memory', label: 'Memory', icon: 'memory', deviceScoped: true },
      { section: 'storage', label: 'Storage', icon: 'disk', deviceScoped: true },
      { section: 'network', label: 'Network', icon: 'network', deviceScoped: true },
      { section: 'sensors', label: 'All sensors', icon: 'thermometer', deviceScoped: true }
    ]
  },
  {
    id: 'system',
    label: 'System',
    deviceScoped: true,
    items: [
      { section: 'containers', label: 'Containers', icon: 'box', deviceScoped: true },
      { section: 'processes', label: 'Processes', icon: 'list', deviceScoped: true },
      { section: 'services', label: 'Services', icon: 'cog', deviceScoped: true },
      { section: 'packages', label: 'Packages', icon: 'package', deviceScoped: true },
      { section: 'logs', label: 'Logs', icon: 'terminal', deviceScoped: true }
    ]
  },
  {
    id: 'customize',
    label: 'Customize',
    deviceScoped: true,
    items: [
      { section: 'security', label: 'Security', icon: 'lock', deviceScoped: true },
      { section: 'remote', label: 'Remote actions', icon: 'terminal', deviceScoped: true },
      { section: 'settings', label: 'Settings', icon: 'settings', deviceScoped: true }
    ]
  }
]

const initial: NavState = { section: 'home', deviceId: null }

function createNavStore() {
  const { subscribe, set, update } = writable<NavState>(initial)

  return {
    subscribe,

    // Selecting a device always moves to its overview: landing on "logs" for a
    // freshly clicked device would be disorienting.
    selectDevice(deviceId: string) {
      set({ section: 'overview', deviceId })
    },

    setSection(next: Section) {
      update((state) => {
        // A device-scoped section with nothing selected is meaningless; send
        // the user to the device list instead of rendering an empty page.
        if (DEVICE_SCOPED.has(next) && !state.deviceId) {
          return { section: 'devices', deviceId: null }
        }
        return { ...state, section: next }
      })
    },

    // Called when a device is deleted or filtered out from under the UI.
    clearDevice() {
      update((state) =>
        DEVICE_SCOPED.has(state.section)
          ? { section: 'devices', deviceId: null }
          : { ...state, deviceId: null }
      )
    },

    reset() {
      set(initial)
    }
  }
}

export const nav = createNavStore()

// Convenience selectors for components that only care about one field.
export const section = derived(nav, ($nav) => $nav.section)
export const selectedDeviceId = derived(nav, ($nav) => $nav.deviceId)

export function currentNav(): NavState {
  return get(nav)
}
