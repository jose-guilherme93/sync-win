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
// serverBase is only used outside the browser's own API calls: the agent
// install command and absolute asset/attachment links. It must stay absolute
// because the install command has to point the agent at a real address
// reachable from outside the browser. The dashboard's own requests stay
// same-origin and never depend on it, so no hardcoded port is needed.
const configured = (import.meta.env.VITE_API_BASE as string | undefined)?.trim()

// Absolute address of the Go server, used for the agent install command.
export const serverBase = configured
  ? configured.replace(/\/+$/, '')
  : window.location.origin

export function apiFetch(input: RequestInfo | URL, init: RequestInit = {}) {
  const method = (init.method || 'GET').toUpperCase()
  const headers = new Headers(init.headers)
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method) && !headers.has('X-LEM-CSRF')) {
    const csrf = document.cookie
      .split(';')
      .map((part) => part.trim())
      .find((part) => part.startsWith('lem_csrf='))
      ?.slice('lem_csrf='.length)
    if (csrf) {
      try {
        headers.set('X-LEM-CSRF', decodeURIComponent(csrf))
      } catch {
        // A malformed cookie must not abort the request; the server will
        // reject it as a missing/invalid CSRF token instead.
      }
    }
  }
  return fetch(input, { ...init, headers, credentials: init.credentials ?? 'include' })
}

// apiURL builds an endpoint URL. It is always relative so the request is
// same-origin: through the Vite proxy in dev and directly against the Go server
// in production (which also serves the dashboard).
export function apiURL(path: string): string {
  return path.startsWith('/') ? path : `/${path}`
}

// streamURL is the SSE endpoint. EventSource cannot send custom headers, so it
// authenticates with a ticket query parameter instead of the CSRF header.
export function streamURL(path: string, ticket: string): string {
  return `${apiURL(path)}?ticket=${encodeURIComponent(ticket)}`
}
