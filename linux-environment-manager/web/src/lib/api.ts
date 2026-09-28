// Shared API base URL.
//
// Production serves the dashboard from the Go server itself, so the API is on
// the same origin and no base URL is needed.
//
// In development the Vite dev server proxies /api to the Go server (see
// vite.config.ts), which keeps the API same-origin too. That removes CORS from
// the dev loop entirely: no allowed-origin list to keep in sync, and no
// cross-origin preflight that a browser can cache a stale failure for.
//
// serverBase stays absolute because the install command has to point the agent
// at a real address reachable from outside the browser.
const configured = (import.meta.env.VITE_API_BASE as string | undefined)?.trim()

// Absolute address of the Go server, used for the agent install command.
export const serverBase = configured
  ? configured.replace(/\/+$/, '')
  : `${window.location.protocol}//${window.location.hostname}:8080`

// When the dev server proxies the API, requests stay relative. VITE_API_BASE is
// deliberately ignored here: it is the host address for the install command,
// not the address the browser should call.
const usesDevProxy = import.meta.env.DEV

export function apiFetch(input: RequestInfo | URL, init: RequestInit = {}) {
  const method = (init.method || 'GET').toUpperCase()
  const headers = new Headers(init.headers)
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method) && !headers.has('X-LEM-CSRF')) {
    const csrf = document.cookie
      .split(';')
      .map((part) => part.trim())
      .find((part) => part.startsWith('lem_csrf='))
      ?.slice('lem_csrf='.length)
    if (csrf) headers.set('X-LEM-CSRF', decodeURIComponent(csrf))
  }
  return fetch(input, { ...init, headers, credentials: init.credentials ?? 'include' })
}

// apiURL builds an endpoint URL. In dev it is relative so the request goes
// through the Vite proxy; in production it is same-origin absolute.
export function apiURL(path: string): string {
  if (!usesDevProxy) return `${serverBase}${path}`
  return path.startsWith('/') ? path : `/${path}`
}

// streamURL is the SSE endpoint. EventSource cannot send custom headers, so it
// authenticates with a ticket query parameter instead of the CSRF header.
export function streamURL(path: string, ticket: string): string {
  const base = usesDevProxy ? '' : serverBase
  return `${base}${path}?ticket=${encodeURIComponent(ticket)}`
}
