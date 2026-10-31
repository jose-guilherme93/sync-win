<script lang="ts">
  // The SyncWin mark: two nodes joined by a transfer path. Inline so it can
  // take the accent colour from `currentColor`; the static copy used outside
  // the app lives in web/public/brand/mark.svg.
  export let size = 20
</script>

<svg
  class="logo"
  width={size}
  height={size}
  viewBox="0 0 32 32"
  fill="none"
  aria-hidden="true"
  focusable="false"
>
  <rect x="3.4" y="3.4" width="9.2" height="9.2" rx="2.7" stroke="currentColor" stroke-width="2.4" />
  <path d="M12.6 8H20.4a4 4 0 0 1 4 4V17" stroke="currentColor" stroke-width="2.4" stroke-linecap="round" />
  <rect x="19.5" y="19.5" width="9.4" height="9.4" rx="2.7" fill="currentColor" />
  <path d="M24.4 19.4 22 16.4 26.8 16.4Z" fill="currentColor" />
</svg>

<style>
  .logo {
    display: block;
    flex-shrink: 0;
  }
</style>
