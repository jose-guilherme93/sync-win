<script lang="ts">
  import { onMount } from 'svelte'
  import { formatBytes, formatRelative } from '../../lib/format'
  import { imageLabel, shortImageId, type DockerImage } from '../../lib/docker'
  import { fetchImageList, inspectImage, pullImage, removeImage } from '../../lib/docker-client'
  import ConfirmDialog from '../ui/ConfirmDialog.svelte'
  import EmptyState from '../ui/EmptyState.svelte'
  import Icon from '../ui/Icon.svelte'
  import Skeleton from '../ui/Skeleton.svelte'

  export let deviceId: string
  export let authHeaders: Record<string, string> = {}
  export let active = false

  // Images are only listed when the panel is the visible one: the Docker
  // screen already asks the device for containers, and an extra command on
  // every visit would queue work nobody is looking at.
  let images: DockerImage[] = []
  let loaded = false
  let loading = false
  let error = ''
  let notice = ''

  let reference = ''
  let pullBusy = false
  let pullError = ''

  let selected: DockerImage | null = null
  let inspectText = ''
  let inspectLoading = false
  let inspectError = ''

  let pendingRemove: DockerImage | null = null
  let removeBusy = false

  let now = Date.now()

  $: if (active && !loaded && deviceId) void load()

  onMount(() => {
    const timer = setInterval(() => (now = Date.now()), 30_000)
    return () => clearInterval(timer)
  })

  async function load() {
    loading = true
    error = ''
    try {
      images = await fetchImageList(deviceId, { headers: authHeaders })
      loaded = true
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not list images'
    } finally {
      loading = false
    }
  }

  async function submitPull(event: SubmitEvent) {
    event.preventDefault()
    const ref = reference.trim()
    if (!ref) return
    pullBusy = true
    pullError = ''
    notice = ''
    try {
      const output = await pullImage(deviceId, ref, { headers: authHeaders })
      notice = output || `Pulled ${ref}`
      reference = ''
      await load()
    } catch (e) {
      pullError = e instanceof Error ? e.message : 'Could not pull the image'
    } finally {
      pullBusy = false
    }
  }

  async function openInspect(image: DockerImage) {
    selected = image
    inspectText = ''
    inspectError = ''
    inspectLoading = true
    try {
      inspectText = await inspectImage(deviceId, imageLabel(image), { headers: authHeaders })
    } catch (e) {
      inspectError = e instanceof Error ? e.message : 'Could not inspect the image'
    } finally {
      inspectLoading = false
    }
  }

  async function confirmRemove() {
    if (!pendingRemove) return
    removeBusy = true
    try {
      await removeImage(deviceId, imageLabel(pendingRemove), pendingRemove.containers > 0, {
        headers: authHeaders
      })
      notice = `Removed ${imageLabel(pendingRemove)}`
      pendingRemove = null
      if (selected) selected = null
      await load()
    } catch (e) {
      error = e instanceof Error ? e.message : 'Could not remove the image'
    } finally {
      removeBusy = false
    }
  }

  function createdLabel(seconds: number): string {
    if (!seconds) return '—'
    return formatRelative(new Date(seconds * 1000).toISOString(), now)
  }

  function sizeLabel(bytes: number): string {
    return bytes > 0 ? formatBytes(bytes) : '—'
  }
</script>

{#if notice}
  <div class="notice" role="status">{notice}</div>
{/if}

<form class="pull" on:submit={submitPull}>
  <label>
    <span class="sr">Image reference</span>
    <input
      bind:value={reference}
      placeholder="nginx:1.27"
      aria-label="Image reference"
      spellcheck="false"
      autocapitalize="off"
      autocomplete="off"
      disabled={pullBusy}
    />
  </label>
  <button type="submit" disabled={pullBusy || !reference.trim()}>
    <Icon name="download" size={14} />
    {pullBusy ? 'Pulling…' : 'Pull'}
  </button>
</form>
{#if pullError}
  <p class="error-inline">{pullError}</p>
{/if}

<div class="head">
  <span class="count">{images.length} image{images.length === 1 ? '' : 's'}</span>
  <button class="ghost" on:click={() => void load()} disabled={loading}>
    <Icon name="refresh" size={13} />
    Refresh
  </button>
</div>

{#if error}
  <p class="error-inline">{error}</p>
{/if}

{#if loading && !loaded}
  <div class="pad"><Skeleton variant="list" rows={4} /></div>
{:else if loaded && images.length === 0}
  <EmptyState
    icon="image"
    title="No images"
    message="The Docker store on this device is empty. Pull one above to get started."
  />
{:else}
  <ul class="rows">
    {#each images as image (image.id)}
      <li class="row" class:sel={selected?.id === image.id}>
        <button class="row-main" on:click={() => void openInspect(image)}>
          <span class="ident">
            <span class="name mono">{imageLabel(image)}</span>
            <span class="tags">{image.repo_tags.length > 1 ? `${image.repo_tags.length} tags` : shortImageId(image)}</span>
          </span>
          <span class="size">{sizeLabel(image.size_bytes)}</span>
          <span class="created">{createdLabel(image.created_at)}</span>
          <span class="used" class:inuse={image.containers > 0}>
            {image.containers > 0 ? `${image.containers} in use` : 'unused'}
          </span>
          <span class="chev"><Icon name="chevron" size={15} /></span>
        </button>
        <div class="row-actions">
          <button class="btn-ghost" on:click={() => void openInspect(image)}>Inspect</button>
          <button class="btn-ghost danger" on:click={() => (pendingRemove = image)}>Remove</button>
        </div>
      </li>
    {/each}
  </ul>
{/if}

{#if selected}
  <section class="inspect" aria-label={`Image ${imageLabel(selected)}`}>
    <header>
      <div>
        <strong class="mono">{imageLabel(selected)}</strong>
        <span class="meta">{shortImageId(selected)} · {sizeLabel(selected.size_bytes)}</span>
      </div>
      <button class="close" on:click={() => (selected = null)} aria-label="Close details">
        <Icon name="x" size={13} />
      </button>
    </header>
    {#if inspectLoading}
      <p class="loading">Reading the image…</p>
    {:else if inspectError}
      <p class="error-inline">{inspectError}</p>
    {:else}
      <pre>{inspectText}</pre>
    {/if}
  </section>
{/if}

<ConfirmDialog
  open={Boolean(pendingRemove)}
  title="Remove image"
  message={pendingRemove
    ? `${imageLabel(pendingRemove)} will be deleted from this device.${pendingRemove.containers > 0 ? ' Containers still using it are removed with it, because the request is forced.' : ''}`
    : ''}
  confirmLabel={removeBusy ? 'Removing…' : 'Remove'}
  destructive
  on:cancel={() => (pendingRemove = null)}
  on:confirm={confirmRemove}
/>

<style>
  .pull {
    display: flex;
    gap: 0.5rem;
    margin: 0 0.9rem 0.6rem;
  }

  .pull label {
    flex: 1;
    display: block;
  }

  .pull input {
    width: 100%;
    height: 34px;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: var(--card-inset);
    color: var(--text);
    font: inherit;
    font-size: 0.8rem;
    padding: 0 0.6rem;
  }

  .pull input:focus {
    outline: none;
    border-color: var(--accent-border);
  }

  .pull button {
    display: inline-flex;
    align-items: center;
    gap: 0.35rem;
    height: 34px;
    padding: 0 0.85rem;
    border: 1px solid var(--accent-border);
    border-radius: var(--radius-sm);
    background: var(--accent-dim);
    color: var(--accent);
    font: inherit;
    font-size: 0.78rem;
    font-weight: 600;
    cursor: pointer;
  }

  .pull button:disabled {
    opacity: 0.6;
    cursor: default;
  }

  .head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 0.9rem 0.5rem;
    color: var(--text-muted);
    font-size: 0.72rem;
  }

  .ghost {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    font: inherit;
    font-size: 0.72rem;
    padding: 0.25rem 0.5rem;
    cursor: pointer;
  }

  .ghost:hover {
    color: var(--text);
    border-color: var(--border-strong);
  }

  .rows {
    list-style: none;
    margin: 0;
    padding: 0 0.6rem 0.8rem;
    display: grid;
    gap: 0.3rem;
  }

  .row {
    display: grid;
    grid-template-columns: 1fr auto;
    align-items: center;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    background: var(--card-inset);
  }

  .row.sel {
    border-color: var(--accent-border);
  }

  .row-main {
    display: grid;
    grid-template-columns: minmax(0, 1.6fr) 0.7fr 0.8fr 0.7fr 24px;
    gap: 0.6rem;
    align-items: center;
    width: 100%;
    padding: 0.5rem 0.6rem;
    border: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    text-align: left;
    cursor: pointer;
  }

  .ident {
    min-width: 0;
    display: grid;
    gap: 0.05rem;
  }

  .name {
    color: var(--text);
    font-size: 0.78rem;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .tags,
  .size,
  .created,
  .used {
    color: var(--text-muted);
    font-size: 0.72rem;
    font-variant-numeric: tabular-nums;
  }

  .used.inuse {
    color: var(--accent);
  }

  .chev {
    color: var(--text-muted);
    display: grid;
    place-items: center;
  }

  .row-actions {
    display: flex;
    gap: 0.3rem;
    padding-right: 0.6rem;
  }

  .btn-ghost {
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text-muted);
    font: inherit;
    font-size: 0.7rem;
    padding: 0.22rem 0.45rem;
    cursor: pointer;
  }

  .btn-ghost:hover {
    color: var(--text);
    border-color: var(--border-strong);
  }

  .btn-ghost.danger:hover {
    color: var(--crit);
    border-color: rgba(248, 113, 113, 0.4);
  }

  .inspect {
    margin: 0 0.9rem 0.9rem;
    border: 1px solid var(--border);
    border-radius: var(--radius);
    background: var(--card-inset);
    overflow: hidden;
  }

  .inspect header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 0.5rem;
    padding: 0.55rem 0.7rem;
    border-bottom: 1px solid var(--border);
  }

  .inspect .meta {
    display: block;
    color: var(--text-muted);
    font-size: 0.7rem;
  }

  .close {
    border: 0;
    background: transparent;
    color: var(--text-muted);
    cursor: pointer;
    padding: 0.15rem;
  }

  .inspect pre {
    margin: 0;
    padding: 0.7rem;
    max-height: 320px;
    overflow: auto;
    font-size: 0.7rem;
    line-height: 1.45;
    color: var(--text-muted);
  }

  .loading {
    margin: 0;
    padding: 0.7rem;
    color: var(--text-muted);
    font-size: 0.75rem;
  }

  .notice {
    margin: 0 0.9rem 0.6rem;
    padding: 0.45rem 0.6rem;
    border: 1px solid var(--accent-border);
    border-radius: var(--radius-sm);
    background: var(--accent-dim);
    color: var(--accent);
    font-size: 0.75rem;
  }

  .error-inline {
    margin: 0 0.9rem 0.6rem;
    color: var(--crit);
    font-size: 0.75rem;
  }

  .pad {
    padding: 0.6rem 0.9rem;
  }

  .sr {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip: rect(0 0 0 0);
  }
</style>
