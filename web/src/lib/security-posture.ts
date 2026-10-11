// Host security posture for the device Security screen. Kept as a standalone
// type module so both the shell (which fetches it) and the screen (which
// renders it) agree on the shape without importing each other.

export type SecurityPosture = {
  publicPorts: { port: number; protocol: string }[]
  pendingUpdates: number
  pendingStatus: string
  failedServices: string[]
  loaded: boolean
  error: string
}
