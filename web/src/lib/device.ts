import type { Device } from './types'

// A selected device has two sources:
//
//   devices[]       the polled list projection. Fresh status, last_seen_at and
//                   scalar telemetry, but no heavy fields.
//   deviceDetails[] the /detail snapshot. Top processes, Docker containers,
//                   logs and security posture, loaded when the device is
//                   selected and refreshed much more slowly.
//
// The screens read one object, so it has to be both: detail-only keys survive
// the spread because the summary does not carry them, while every field the
// summary does carry (including hardware) wins. Merging only the hardware would
// leave last_seen_at frozen, which is what made the topbar say "stale" while
// Home said "online".
export function mergeDeviceDetail(detail: Device | undefined, live: Device | undefined): Device | null {
  if (!live) return detail ?? null
  if (!detail) return live
  return {
    ...detail,
    ...live,
    hardware:
      detail.hardware && live.hardware
        ? { ...detail.hardware, ...live.hardware }
        : live.hardware ?? detail.hardware
  }
}
