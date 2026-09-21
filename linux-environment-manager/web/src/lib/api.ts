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
