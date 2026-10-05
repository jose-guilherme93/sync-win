<script lang="ts">
  // Loading placeholder. Shaped as a card grid so the first paint has the same
  // silhouette as the content it is standing in for, which stops the layout
  // from jumping when the data arrives.
  export let rows = 3
  export let variant: 'cards' | 'list' | 'lines' = 'cards'
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

  .w40 { width: 40%; }
  .w60 { width: 60%; }
  .w70 { width: 70%; }
  .w90 { width: 90%; }

  @keyframes sk-shimmer {
    from { background-position: 100% 0; }
    to { background-position: -100% 0; }
  }

  @media (prefers-reduced-motion: reduce) {
    .sk-line { animation: none; }
  }
</style>
