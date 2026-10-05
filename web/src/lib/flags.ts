// Feature flags for screens whose backing API does not exist yet.
//
// The plan calls for the UI to be buildable ahead of the backend. Rather than
// faking data unconditionally (which would look like a bug), screens that lack
// an endpoint render an honest "not collected yet" state unless the matching
// flag is on, in which case they show clearly-labelled sample data so the
// layout can be reviewed.
//
// Set these in `web/.env.local` to preview a screen:
//   VITE_MOCK_SERVICES=true

function flag(value: unknown): boolean {
  return String(value ?? '').toLowerCase() === 'true'
}

export const MOCK = {
  // GET /api/devices/{id}/services
  services: flag(import.meta.env.VITE_MOCK_SERVICES),
  // GET /api/devices/{id}/ports
  ports: flag(import.meta.env.VITE_MOCK_PORTS),
  // GET /api/devices/{id}/smart
  smart: flag(import.meta.env.VITE_MOCK_SMART)
}
