// Host security posture for the device Security screen. Kept as a standalone
// type module so both the shell (which fetches it) and the screen (which
// renders it) agree on the shape without importing each other.
import type { OpenPort } from './ports'

export type SecurityPosture = {
  // Full socket records, not just port and protocol: two sockets can share those
  // and the screen keys its list on the whole record, so trimming it here is
  // what made a duplicated socket blank the section.
  publicPorts: OpenPort[]
  pendingUpdates: number
  pendingStatus: string
  failedServices: string[]
  loaded: boolean
  error: string
}
