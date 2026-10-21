// Tab bookkeeping for the remote-access terminal.
//
// The dashboard keeps one SSH session per tab. The server documents a ceiling
// of three concurrent sessions per device (`.agent-memory/decisions/remote-access.md`),
// so the tab strip stops offering a fourth; the server remains the authority if
// the limit ever changes.

export const MAX_TERMINAL_TABS = 3

export type TerminalTabStatus = 'connecting' | 'connected' | 'closed' | 'error'

export type TerminalTab = {
  id: string
  user: string
  label: string
  status: TerminalTabStatus
  error: string
}

export function canOpenTerminal(tabs: readonly TerminalTab[]): boolean {
  return tabs.length < MAX_TERMINAL_TABS
}

export function addTerminalTab(tabs: TerminalTab[], tab: TerminalTab): TerminalTab[] {
  if (!canOpenTerminal(tabs)) return tabs
  return [...tabs, tab]
}

export function closeTerminalTab(tabs: TerminalTab[], id: string): TerminalTab[] {
  return tabs.filter((tab) => tab.id !== id)
}

// nextActiveTerminal decides which tab takes focus when `closingId` goes away:
// the tab that slid into its place, or the one before it when the last tab
// closes. An unknown id keeps the first remaining tab rather than blanking the
// panel.
export function nextActiveTerminal(tabs: readonly TerminalTab[], closingId: string): string {
  const index = tabs.findIndex((tab) => tab.id === closingId)
  const remaining = tabs.filter((tab) => tab.id !== closingId)
  if (remaining.length === 0) return ''
  if (index === -1) return remaining[0].id
  return remaining[Math.min(index, remaining.length - 1)].id
}

// nextTerminalLabel names a new tab after its account, adding a counter when
// the same account is already open so the tabs stay distinguishable. The label
// is fixed at creation: closing a neighbouring tab must not rename a running
// session under the user.
export function nextTerminalLabel(tabs: readonly TerminalTab[], user: string): string {
  const account = user.trim() || 'default'
  const taken = new Set(tabs.map((tab) => tab.label))
  if (!taken.has(account)) return account
  for (let count = 2; ; count++) {
    const candidate = `${account} (${count})`
    if (!taken.has(candidate)) return candidate
  }
}
