import { defineConfig } from 'vitest/config'
import { svelte } from '@sveltejs/vite-plugin-svelte'
import { svelteTesting } from '@testing-library/svelte/vite'

// Test runner configuration for the dashboard. Kept separate from
// vite.config.ts so the production build is untouched. `svelteTesting()`
// registers the browser resolve conditions Svelte 5 components need, and jsdom
// provides window/document. IndexedDB is provided by fake-indexeddb in
// src/test-setup.ts.
export default defineConfig({
  plugins: [svelte(), svelteTesting()],
  test: {
    environment: 'jsdom',
    include: ['src/**/*.{test,spec}.{ts,js}'],
    setupFiles: ['./src/test-setup.ts'],
    restoreMocks: true,
    clearMocks: true
  }
})
