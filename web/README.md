# SyncWin Web Dashboard

The web frontend for SyncWin.

## Stack

- Svelte 5
- TypeScript 6
- Vite 8
- Chart.js 4 (telemetry charts)

## Development

```bash
npm install
npm run dev
```

This starts the Vite dev server with HMR. Point it at a running SyncWin server by configuring the API base URL.

## Production build

```bash
npm run build
```

Output goes to `dist/`. In production, the Go server serves this directory from `SYNCWIN_WEB_DIR` (default `/app/web`) on the same origin and port.

## Components

| File | Purpose |
|------|---------|
| `App.svelte` | Root: auth, device grid, dashboard layout, polling |
| `DeviceModal.svelte` | Device detail with tabs: System, Files, Packages, Saves, Notes, Docker |
| `DockerTab.svelte` | Docker container management (list, actions, exec, compose, prune) |
| `DashboardCharts.svelte` | CPU/memory/disk I/O charts (Chart.js) |
| `SimpleMetrics.svelte` | Device summary cards with save badges |
| `SystemMetrics.svelte` | Detailed hardware telemetry display |
| `HistoryModal.svelte` | Telemetry history with time period selection |
| `NotificationsModal.svelte` | Notification provider settings and inbox |
| `NotificationToast.svelte` | Real-time toast notifications via SSE |

## Libraries

| File | Purpose |
|------|---------|
| `lib/telemetry-store.ts` | Telemetry state management |
| `lib/telemetry-cache.ts` | 60-second TTL cache for telemetry data |

## Architecture notes

- The dashboard is account-first: the entry screen is sign in / create account.
- Authentication uses the server's HttpOnly `syncwin_session` cookie; mutating requests include the CSRF header through `apiFetch`.
- A short-lived, owner-bound ticket is used for the notification SSE stream.
- The dashboard polls the server for device updates and telemetry.
- Large app inventories use lazy loading and search to keep interactions smooth.
- Telemetry charts update in real-time with a 60-second cache.
- Docker commands flow through the server command queue to agents.
- Notifications are pushed in real-time via Server-Sent Events (SSE).
