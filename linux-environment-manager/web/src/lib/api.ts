// Shared API base URL.
//
// By default the dashboard talks to the Go server on port 8080 of the same
// host that served the page. Set VITE_API_BASE to point the UI at a different
// server (a remote instance, or a dev stack published on another port):
//
//   VITE_API_BASE=http://localhost:8081 npm run dev
//
const configured = (import.meta.env.VITE_API_BASE as string | undefined)?.trim()

export const serverBase = configured
  ? configured.replace(/\/+$/, '')
  : `${window.location.protocol}//${window.location.hostname}:8080`

// Keep the session cookie on every dashboard request, including the Vite dev
// server where the API is on a different port.
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
