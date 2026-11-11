<script lang="ts">
  // Loading placeholder. Shaped as a card grid so the first paint has the same
  // silhouette as the content it is standing in for, which stops the layout
  // from jumping when the data arrives.
  export let rows = 3
  export let variant: 'cards' | 'list' | 'lines' | 'chart' = 'cards'
  // Chart skeletons stand in for a plot. The shape matches what lands there: an
  // area chart with a filled silhouette, not a bar chart. A row of bars under a
  // line chart promises the wrong thing for the second it is on screen.
  const CHART_PATH =
    'M0 30 L5 26 L10 29 L15 23 L20 26 L25 20 L30 24 L35 19 L40 27 L45 22 ' +
    'L50 17 L55 23 L60 26 L65 18 L70 22 L75 25 L80 20 L85 24 L90 19 L95 23 L100 21'
</script>

{#if variant === 'cards'}
  <div class="sk-grid" aria-hidden="true">
    {#each Array(rows) as _, i (i)}
      <div class="sk-card">
        <div class="sk-line w40"></div>
        <div class="sk-line w70 tall"></div>
        <div class="sk-line w90"></div>
        <div class="sk-line w60"></div>
      </div>
    {/each}
  </div>
{:else if variant === 'list'}
  <div class="sk-list" aria-hidden="true">
    {#each Array(rows) as _, i (i)}
      <div class="sk-row">
        <div class="sk-line w40"></div>
        <div class="sk-line w70"></div>
        <div class="sk-line w90"></div>
      </div>
    {/each}
  </div>
{:else if variant === 'chart'}
  <div class="sk-chart" aria-hidden="true">
    <svg viewBox="0 0 100 40" preserveAspectRatio="none">
      <defs>
        <linearGradient id="sk-chart-fill" x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stop-color="rgba(52, 211, 153, 0.26)" />
          <stop offset="100%" stop-color="rgba(52, 211, 153, 0.02)" />
        </linearGradient>
      </defs>
      <path d="{CHART_PATH} L100 40 L0 40 Z" fill="url(#sk-chart-fill)" />
      <path d={CHART_PATH} fill="none" stroke="rgba(52, 211, 153, 0.45)" stroke-width="0.7" />
    </svg>
  </div>
{:else}
  <div class="sk-lines" aria-hidden="true">
    {#each Array(rows) as _, i (i)}
      <div class="sk-line" style="width: {45 + ((i * 17) % 45)}%"></div>
    {/each}
  </div>
{/if}

<style>
  .sk-line {
    height: 0.7rem;
    border-radius: 6px;
    background: linear-gradient(
      90deg,
      rgba(148, 163, 184, 0.07) 25%,
      rgba(148, 163, 184, 0.18) 37%,
      rgba(148, 163, 184, 0.07) 63%
    );
    background-size: 400% 100%;
    animation: sk-shimmer 1.4s linear infinite;
  }

  .sk-line.tall { height: 1.4rem; }

  .sk-grid {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: 0.75rem;
  }

  .sk-card {
    display: grid;
    gap: 0.6rem;
    padding: 0.9rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
  }

  .sk-list { display: grid; gap: 0.5rem; }

  .sk-row {
    display: grid;
    grid-template-columns: 1fr 1fr 1fr;
    gap: 0.6rem;
    padding: 0.75rem;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
  }

  .sk-lines { display: grid; gap: 0.6rem; }

  .sk-chart {
    position: relative;
    height: 100%;
    min-height: 120px;
    border-radius: var(--radius-sm);
    background: repeating-linear-gradient(
      180deg,
      transparent 0,
      transparent 24%,
      rgba(148, 163, 184, 0.08) 25%
    );
    overflow: hidden;
  }

  .sk-chart svg {
    width: 100%;
    height: 100%;
    display: block;
    animation: sk-chart-pulse 1.8s ease-in-out infinite;
  }

  @keyframes sk-chart-pulse {
    0%, 100% { opacity: 0.55; }
    50% { opacity: 1; }
  }


  .w40 { width: 40%; }
  .w60 { width: 60%; }
  .w70 { width: 70%; }
  .w90 { width: 90%; }

  @keyframes sk-shimmer {
    from { background-position: 100% 0; }
    to { background-position: -100% 0; }
  }

  @media (prefers-reduced-motion: reduce) {
    .sk-line,
    .sk-chart svg { animation: none; }
  }
</style>
