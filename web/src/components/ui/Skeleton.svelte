<script lang="ts">
  // Loading placeholder. Shaped as a card grid so the first paint has the same
  // silhouette as the content it is standing in for, which stops the layout
  // from jumping when the data arrives.
  export let rows = 3
  export let variant: 'cards' | 'list' | 'lines' | 'chart' = 'cards'
  // Chart skeletons stand in for a plot: the shimmering bars read as "a series
  // is loading" instead of "this device reported nothing", which is the
  // difference between waiting and a wrong answer.
  const CHART_BARS = [38, 52, 44, 61, 47, 70, 58, 66, 49, 74, 62, 55, 68, 51, 79, 64, 57, 71, 60, 83, 69, 63, 76, 58]
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
    {#each CHART_BARS as value, i (i)}
      <span class="sk-bar" style="height: {value}%"></span>
    {/each}
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
    display: flex;
    align-items: flex-end;
    gap: 2.5%;
    height: 100%;
    min-height: 120px;
    padding: 0 2%;
    border-radius: var(--radius-sm);
    background: repeating-linear-gradient(
      180deg,
      transparent 0,
      transparent 24%,
      rgba(148, 163, 184, 0.1) 25%
    );
  }

  .sk-bar {
    flex: 1;
    min-width: 2px;
    border-radius: 3px 3px 0 0;
    background: linear-gradient(
      180deg,
      rgba(52, 211, 153, 0.2),
      rgba(52, 211, 153, 0.05)
    );
    animation: sk-bar-shimmer 1.6s ease-in-out infinite;
  }

  .sk-bar:nth-child(3n) { animation-delay: 0.15s; }
  .sk-bar:nth-child(4n) { animation-delay: 0.3s; }
  .sk-bar:nth-child(5n) { animation-delay: 0.45s; }

  @keyframes sk-bar-shimmer {
    0%, 100% { opacity: 0.65; }
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
    .sk-bar { animation: none; }
  }
</style>
