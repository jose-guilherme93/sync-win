import { defineConfig } from 'vite'
import { svelte } from '@sveltejs/vite-plugin-svelte'

// The Go API is proxied in development so the dashboard and the API share an
// origin. Cross-origin requests in dev meant every endpoint needed a CORS
// allowlist entry and a preflight, and a browser that cached a preflight
// failure kept failing after the server was fixed.
//
// The proxy target is resolved inside the dev container, so it must be the
// compose service address. VITE_API_BASE is what the *browser* uses and is a
// host address, which is not reachable from here.
const apiTarget = process.env.VITE_PROXY_TARGET || 'http://server:8080'

// Extra hostnames the dev server may answer to, comma-separated. Vite allows
// only localhost, *.localhost and IP addresses by default, so reaching the
// dashboard through a MagicDNS name (over Tailscale, say) is refused with
// "Blocked request" unless the name is listed here. An explicit list is used
// rather than `true`: `allowedHosts: true` reopens the DNS-rebinding hole the
// default closes (GHSA-vg6x-rcgg-rjx6), which would let any site read the
// source served from a developer's machine.
const allowedHosts = (process.env.VITE_ALLOWED_HOSTS || '')
  .split(',')
  .map((host) => host.trim())
  .filter(Boolean)

// https://vite.dev/config/
export default defineConfig({
  plugins: [svelte()],
  server: {
    allowedHosts,
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
        // The remote-access terminal is a WebSocket under /api. Without ws:true
        // the proxy forwards the HTTP handshake but not the upgrade, so the
        // terminal never connects in dev.
        ws: true,
        // The notification stream is a long-lived SSE connection; proxying it
        // must not buffer or time out.
        configure: (proxy) => {
          proxy.on('proxyRes', (proxyRes) => {
            if (proxyRes.headers['content-type']?.includes('text/event-stream')) {
              proxyRes.headers['cache-control'] = 'no-cache, no-transform'
            }
          })
        }
      }
    }
  }
})
