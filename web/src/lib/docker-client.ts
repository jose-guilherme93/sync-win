// Client for the Docker request/response endpoints.
//
// The dashboard never talks to the Docker socket. It queues a command on the
// server; the agent polls the queue from its own machine and posts the result
// back. While the result is pending the server answers 202 with an empty body,
// so every command needs a short, adaptive poll — polling once a second made
// the old screen feel slower than the agent actually was.
//
// State is delivered two ways: a cached snapshot on demand and an SSE stream
// the server broadcasts whenever the agent reports something new. Both carry
// the same shape, so callers feed both through `normalizeDockerState`.

import { apiFetch, apiURL } from './api'
import {
  friendlyDockerError,
  normalizeDockerState,
  parseContainerList,
  type DockerCommandResult,
  type DockerContainer,
  type DockerState
} from './docker'

export type DockerRequestOptions = {
  /** Total time allowed for the device to answer. */
  timeoutMs?: number
  signal?: AbortSignal
  /** Bearer header for API clients; the dashboard's cookie session needs none. */
  headers?: Record<string, string>
}

const DEFAULT_TIMEOUT_MS = 45_000
const FIRST_POLL_MS = 250
const MAX_POLL_MS = 1_500

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms))
}

async function readError(res: Response): Promise<string> {
  try {
    const payload = await res.json()
    if (payload && typeof payload.error === 'string') return payload.error
  } catch {
    // Fall through to the status line.
  }
  return `request failed (${res.status})`
}

export async function fetchDockerState(
  deviceId: string,
  options: DockerRequestOptions = {}
): Promise<DockerState> {
  const res = await apiFetch(apiURL(`/api/devices/${deviceId}/docker`), {
    signal: options.signal,
    headers: options.headers
  })
  if (!res.ok) throw new Error(await readError(res))
  return normalizeDockerState(await res.json())
}

export async function enqueueDockerRequest(
  deviceId: string,
  type: string,
  target = '',
  payload = '',
  headers: Record<string, string> = {}
): Promise<string> {
  const res = await apiFetch(apiURL(`/api/devices/${deviceId}/docker`), {
    method: 'POST',
    headers: { ...headers, 'Content-Type': 'application/json' },
    body: JSON.stringify({ type, target, payload })
  })
  if (!res.ok) throw new Error(await readError(res))
  const body = await res.json()
  if (!body || typeof body.id !== 'string' || !body.id) {
    throw new Error('The server did not return a request id')
  }
  return body.id
}

export async function waitForDockerResult(
  deviceId: string,
  requestId: string,
  options: DockerRequestOptions = {}
): Promise<DockerCommandResult> {
  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS
  const deadline = Date.now() + timeoutMs
  let delay = FIRST_POLL_MS

  for (;;) {
    if (options.signal?.aborted) throw new DOMException('Aborted', 'AbortError')

    let res: Response | null = null
    try {
      res = await apiFetch(apiURL(`/api/devices/${deviceId}/docker/result/${requestId}`), {
        signal: options.signal,
        headers: options.headers
      })
    } catch (error) {
      if (error instanceof DOMException && error.name === 'AbortError') throw error
      // A transient network failure must not consume the wait: the request may
      // still complete on the device. Keep polling until the deadline.
      res = null
    }

    if (res && res.status === 200) {
      const payload = await res.json()
      if (payload && (payload.status === 'completed' || payload.status === 'failed')) {
        return payload as DockerCommandResult
      }
      throw new Error('The server returned an unexpected command result')
    }
    if (res && res.status !== 202 && res.status !== 204) {
      throw new Error(await readError(res))
    }

    if (Date.now() >= deadline) {
      throw new Error(
        `The device did not answer within ${Math.round(timeoutMs / 1000)}s. It may be offline or busy.`
      )
    }

    await sleep(Math.max(0, Math.min(delay, deadline - Date.now())))
    delay = Math.min(Math.round(delay * 1.5), MAX_POLL_MS)
  }
}

// Runs one command and resolves with its result. `failed` results become an
// Error carrying the device's reason, so a call site cannot accidentally treat
// a failed action as done.
export async function runDockerCommand(
  deviceId: string,
  type: string,
  target = '',
  payload = '',
  options: DockerRequestOptions = {}
): Promise<DockerCommandResult> {
  const requestId = await enqueueDockerRequest(deviceId, type, target, payload, options.headers)
  const result = await waitForDockerResult(deviceId, requestId, options)
  if (result.status === 'failed') {
    throw new Error(friendlyDockerError(result.message, type))
  }
  return result
}

// The list command is the only way to learn the truth right now: the cached
// snapshot is sampled by the agent roughly once a minute. Asking the device is
// what makes the Refresh button mean something.
export async function fetchContainerList(
  deviceId: string,
  options: DockerRequestOptions = {}
): Promise<DockerContainer[]> {
  const result = await runDockerCommand(deviceId, 'list', '', '', options)
  return parseContainerList(result.message)
}

export type DockerStreamHandlers = {
  onState: (state: DockerState) => void
  onOpen?: () => void
  onError?: () => void
}

// Opens the live state stream and reconnects with backoff. Returns a close
// function. When EventSource is unavailable (tests, very old browsers) it does
// nothing and reports the failure to the caller instead of throwing.
export function openDockerStream(deviceId: string, handlers: DockerStreamHandlers): () => void {
  if (typeof EventSource === 'undefined') {
    handlers.onError?.()
    return () => {}
  }

  let closed = false
  let source: EventSource | null = null
  let retryTimer: ReturnType<typeof setTimeout> | null = null
  let failures = 0

  const connect = () => {
    if (closed) return
    source = new EventSource(apiURL(`/api/devices/${deviceId}/docker/stream`), { withCredentials: true })

    source.onopen = () => {
      failures = 0
      handlers.onOpen?.()
    }

    source.onmessage = (event) => {
      try {
        handlers.onState(normalizeDockerState(JSON.parse(event.data)))
      } catch {
        // A malformed frame is not worth tearing the stream down; the next
        // broadcast carries a complete state.
      }
    }

    source.onerror = () => {
      source?.close()
      source = null
      handlers.onError?.()
      if (closed) return
      failures += 1
      const delay = Math.min(2_000 * 2 ** (failures - 1), 30_000)
      retryTimer = setTimeout(connect, delay)
    }
  }

  connect()

  return () => {
    closed = true
    if (retryTimer) clearTimeout(retryTimer)
    source?.close()
    source = null
  }
}
