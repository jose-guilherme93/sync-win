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
  | 'alerts'
  | 'findings'
  | 'reports'
  // Device-scoped — hardware
  | 'overview'
  | 'cpu'
  | 'memory'
  | 'storage'
  | 'network'
  | 'topology'
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
  // Fleet-wide administration
  | 'configuration'
  | 'notifications'

export type NavState = {
  section: Section
  deviceId: string | null
}

// Sections that only make sense with a device selected. Used to decide whether
// a click in the sidebar should be allowed and whether to fall back to the
// fleet view when the selected device disappears.
export const DEVICE_SCOPED: ReadonlySet<Section> = new Set<Section>([
  'overview', 'cpu', 'memory', 'storage', 'network', 'topology', 'sensors',
  'containers', 'processes', 'services', 'packages', 'logs',
  'security', 'remote', 'settings'
])

// The fleet sidebar is data-driven: it renders these groups in order and marks
// the entry whose `section` matches the active one. Device sections are not
// here at all — they belong to the device screen's tab row, so the fleet
// sidebar never shows a menu for something that is not open.
export type NavItem = { section: Section; label: string; icon: string }
export type NavGroup = { id: string; label: string; items: NavItem[] }

export const NAV_GROUPS: NavGroup[] = [
  {
    id: 'fleet',
    label: '',
    items: [
      { section: 'home', label: 'Home', icon: 'grid' },
      { section: 'alerts', label: 'Alerts', icon: 'bell' },
      { section: 'findings', label: 'Findings', icon: 'shield' },
      { section: 'reports', label: 'Reports', icon: 'chart' }
    ]
  },
  {
    id: 'admin',
    label: 'Administration',
    items: [
      { section: 'notifications', label: 'Notifications', icon: 'bell' },
      { section: 'configuration', label: 'Settings', icon: 'settings' }
    ]
  }
]

// The device screen is a place of its own: opening a device leaves the fleet
// behind, hides the sidebar and puts every section in one horizontally
// scrollable row. Groups only add a separator; there are no nested menus.
export type DeviceTab = { section: Section; label: string; icon: string }
export type DeviceTabGroup = { id: string; label: string; items: DeviceTab[] }

export const DEVICE_TABS: DeviceTabGroup[] = [
  {
    id: 'hardware',
    label: 'Hardware',
    items: [
      { section: 'overview', label: 'Overview', icon: 'gauge' },
      { section: 'cpu', label: 'CPU & Thermal', icon: 'cpu' },
      { section: 'memory', label: 'Memory', icon: 'memory' },
      { section: 'storage', label: 'Storage', icon: 'disk' },
      { section: 'network', label: 'Network', icon: 'network' },
      { section: 'topology', label: 'Topology', icon: 'topology' },
      { section: 'sensors', label: 'Sensors', icon: 'thermometer' }
    ]
  },
  {
    id: 'system',
    label: 'System',
    items: [
      { section: 'containers', label: 'Docker', icon: 'box' },
      { section: 'processes', label: 'Processes', icon: 'list' },
      { section: 'services', label: 'Services', icon: 'cog' },
      { section: 'packages', label: 'Packages', icon: 'package' },
      { section: 'logs', label: 'Logs', icon: 'terminal' }
    ]
  },
  {
    id: 'security',
    label: 'Access',
    items: [
      { section: 'security', label: 'Security', icon: 'lock' },
      { section: 'remote', label: 'Remote access', icon: 'terminal' },
      { section: 'settings', label: 'Settings', icon: 'settings' }
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
        // the user back to the fleet instead of rendering an empty page.
        if (DEVICE_SCOPED.has(next) && !state.deviceId) {
          return { section: 'home', deviceId: null }
        }
        return { ...state, section: next }
      })
    },

    // Leaving the device screen: back to the fleet, no device selected.
    clearDevice() {
      set({ section: 'home', deviceId: null })
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
