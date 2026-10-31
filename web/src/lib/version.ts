// Version comparison for the agent badge.
//
// The agent reports a plain semver string ("0.11.2"). The server advertises the
// version of the binary it hands out, which is what a device should be running.
// A device that is behind keeps working — an older agent simply collects less —
// so this is only ever used to explain an empty panel, never to hide one.

export function compareVersions(a: string, b: string): number {
  const parse = (value: string): number[] =>
    value
      .replace(/^v/, '')
      .split('.')
      .map((part) => {
        const digits = part.match(/^\d+/)
        return digits ? Number(digits[0]) : 0
      })
  const left = parse(a)
  const right = parse(b)
  const length = Math.max(left.length, right.length)
  for (let i = 0; i < length; i += 1) {
    const diff = (left[i] ?? 0) - (right[i] ?? 0)
    if (diff !== 0) return diff < 0 ? -1 : 1
  }
  return 0
}

/** True when both versions are known and the device's is behind the server's. */
export function isAgentOutdated(deviceVersion: string | undefined, shippedVersion: string | undefined): boolean {
  if (!deviceVersion || !shippedVersion) return false
  if (!/^\d+\.\d+/.test(deviceVersion) || !/^\d+\.\d+/.test(shippedVersion)) return false
  return compareVersions(deviceVersion, shippedVersion) < 0
}
