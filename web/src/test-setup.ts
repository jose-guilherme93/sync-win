// Shared Vitest setup for the dashboard.
//
// fake-indexeddb installs an in-memory IndexedDB on `globalThis` so the
// telemetry cache can be exercised under jsdom.
import 'fake-indexeddb/auto'

// jsdom does not implement matchMedia; some components probe it for theme or
// reduced-motion. Provide a no-op so mounting them does not throw.
if (typeof window !== 'undefined' && !window.matchMedia) {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener() {},
    removeListener() {},
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent() {
      return false
    }
  })) as unknown as typeof window.matchMedia
}
