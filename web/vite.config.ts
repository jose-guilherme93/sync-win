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

// https://vite.dev/config/
export default defineConfig({
  plugins: [svelte()],
  server: {
    proxy: {
      '/api': {
        target: apiTarget,
        changeOrigin: true,
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
