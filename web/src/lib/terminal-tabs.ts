// Tab bookkeeping for the remote-access terminal.
//
// The dashboard keeps one SSH session per tab. Six is the ceiling the agent's
// contract enforces (`max_sessions` in agent/internal/contract/contract.json),
// so the tab strip stops offering a seventh; the agent remains the authority.

export const MAX_TERMINAL_TABS = 6

export type TerminalTabStatus = 'connecting' | 'connected' | 'closed' | 'error'

export type TerminalTab = {
  id: string
  user: string
  // label is the account fallback; title is the program's current window title
  // (OSC 0/2), so a tab running btop reads "btop".
  label: string
  title: string
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

// terminalAccountLabel is the fallback shown until the shell or a program sets
// a window title. It is deliberately unnumbered: the live title, not a counter,
// is what tells two sessions apart.
export function terminalAccountLabel(user: string): string {
  return user.trim() || 'default'
}
