// Client for the process endpoints (docs/process-tab-spec.md §2).
//
// Reads are plain GETs. Mutations are POSTs that the server gates
// (SYNCWIN_ENABLE_REMOTE_MUTATIONS plus the agent policy) and audits; the web
// validates the same ranges locally so an invalid value never reaches the
// device, but the server and the agent remain the authority.

import { apiFetch, apiURL } from './api'
import { HISTORY_RANGES, SIGNALS, historyWindowMs, parseProcessHistory, parseProcessSnapshot } from './processes'
import type { ProcessHistory, ProcessSignal, ProcessSnapshot } from './types'

export type ProcessRequestOptions = {
  signal?: AbortSignal
  /** Bearer header for API clients; the dashboard's cookie session needs none. */
  headers?: Record<string, string>
}

// Same bounds as the agent guard. pid 1 is never signalable.
const MIN_PID = 2
const MAX_PID = 4_194_304

async function readError(res: Response): Promise<string> {
  try {
    const payload = await res.json()
    if (payload && typeof payload.error === 'string' && payload.error) return payload.error
  } catch {
    // Fall through to the status line: an empty body is not a reason.
  }
  return `request failed (${res.status})`
}

async function getJSON(path: string, options: ProcessRequestOptions): Promise<unknown> {
  const res = await apiFetch(apiURL(path), { signal: options.signal, headers: options.headers })
  if (!res.ok) throw new Error(await readError(res))
  return res.json()
}

async function post(
  path: string,
  body: Record<string, unknown>,
  options: ProcessRequestOptions
): Promise<void> {
  const res = await apiFetch(apiURL(path), {
    method: 'POST',
    headers: { ...options.headers, 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal: options.signal
  })
  // 202 means queued for the agent: accepted, not finished.
  if (!res.ok) throw new Error(await readError(res))
}

function assertPid(pid: number): void {
  if (!Number.isInteger(pid) || pid < MIN_PID || pid > MAX_PID) {
    throw new Error('pid out of range')
  }
}

// GET /api/devices/{id}/processes
export async function fetchProcessSnapshot(
  deviceId: string,
  options: ProcessRequestOptions = {}
): Promise<ProcessSnapshot> {
  return parseProcessSnapshot(await getJSON(`/api/devices/${deviceId}/processes`, options))
}

// GET /api/devices/{id}/processes/history?range=1h|24h|7d
export async function fetchProcessHistory(
  deviceId: string,
  range: string,
  options: ProcessRequestOptions = {}
): Promise<ProcessHistory> {
  if (!(HISTORY_RANGES as readonly string[]).includes(range)) {
    throw new Error(`range not supported: ${range}`)
  }
  // The route takes the range name; the window length is only needed locally.
  void historyWindowMs(range)
  const payload = await getJSON(
    `/api/devices/${deviceId}/processes/history?range=${encodeURIComponent(range)}`,
    options
  )
  return parseProcessHistory(payload)
}

// POST /api/devices/{id}/processes/{pid}/signal
// `startTimeUnix` is the start instant of the snapshot the user saw. The agent
// re-reads it and rejects the command if the pid has been reused since.
export async function signalProcess(
  deviceId: string,
  pid: number,
  signal: ProcessSignal,
  startTimeUnix: number,
  options: ProcessRequestOptions = {}
): Promise<void> {
  if (!SIGNALS.includes(signal)) throw new Error(`signal not allowed: ${signal}`)
  assertPid(pid)
  if (!Number.isInteger(startTimeUnix) || startTimeUnix <= 0) {
    throw new Error('start time missing from the snapshot')
  }
  await post(`/api/devices/${deviceId}/processes/${pid}/signal`, { signal, start_time_unix: startTimeUnix }, options)
}

// POST /api/devices/{id}/processes/{pid}/nice
export async function reniceProcess(
  deviceId: string,
  pid: number,
  nice: number,
  options: ProcessRequestOptions = {}
): Promise<void> {
  assertPid(pid)
  if (!Number.isInteger(nice) || nice < -20 || nice > 19) throw new Error('nice out of range')
  await post(`/api/devices/${deviceId}/processes/${pid}/nice`, { nice }, options)
}

// POST /api/devices/{id}/processes/{pid}/ionice
export async function ioniceProcess(
  deviceId: string,
  pid: number,
  ioClass: number,
  level: number,
  options: ProcessRequestOptions = {}
): Promise<void> {
  assertPid(pid)
  if (!Number.isInteger(ioClass) || ioClass < 1 || ioClass > 3) throw new Error('ionice class out of range')
  if (!Number.isInteger(level) || level < 0 || level > 7) throw new Error('ionice level out of range')
  await post(`/api/devices/${deviceId}/processes/${pid}/ionice`, { class: ioClass, level }, options)
}
