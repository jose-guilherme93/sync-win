// How an event reads in the dashboard.
//
// The stored message carries the icon and the device name, because that is
// what an external channel needs: a Telegram message has to stand on its own.
// The dashboard already shows the icon and the device name next to the
// message, so the prefix is stripped before it is rendered.

const PREFIX = /^\s*\S+\s*\[[^\]]*\]\s*/

/** The message without the icon and the device name. */
export function eventDetail(message: string): string {
  const text = (message ?? '').trim()
  return text.replace(PREFIX, '').trim() || text
}

/** The icon for an event type, matching the one the server uses. */
export function eventIcon(type: string): string {
  switch (type) {
    case 'device_offline': return 'alert'
    case 'device_online': return 'check'
    case 'sync_error': return 'alert'
    case 'device_enrolled': return 'plus'
    default: return 'bell'
  }
}
