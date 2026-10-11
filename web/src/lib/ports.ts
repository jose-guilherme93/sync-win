// Listening sockets as the agent reports them, plus the identity that both
// de-duplicates them and keys the lists that render them.
//
// The agent can report the same socket twice: `ss` lists a UDP socket once per
// bound address, and the collector folds `0.0.0.0`, `*` and an empty host onto
// the same value, so two rows arrive byte-identical. Every screen that renders
// these uses a keyed {#each}, and Svelte throws `each_key_duplicate` on a
// repeated key. That does not skip a row: it aborts the component's render and
// leaves the previous DOM frozen in place. One duplicated socket blanked
// Services, Network and Security on the same device.
//
// The key and the guard therefore have to agree, so both come from one identity
// defined here rather than a different template literal per screen. The agent
// de-duplicates at the source too, but a device running an older agent still
// sends the repeats, and the dashboard must not go blank because of it.

export type OpenPort = {
  protocol: string
  local_address: string
  port: number
  process?: string
  pid?: number
}

// Identity of a listening socket. `process` and `pid` are part of it: the same
// address held by two different processes is two real sockets, not a repeat, and
// collapsing them would hide one.
export function portIdentity(port: OpenPort): string {
  return [port.protocol, port.local_address, port.port, port.process ?? '', port.pid ?? ''].join('|')
}

// Drops the rows the agent reported more than once. A list that has been through
// this is safe to render with portIdentity as its key.
export function uniquePorts(ports: OpenPort[]): OpenPort[] {
  const seen = new Set<string>()
  return ports.filter((port) => {
    const identity = portIdentity(port)
    if (seen.has(identity)) return false
    seen.add(identity)
    return true
  })
}
