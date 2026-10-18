<script lang="ts">
  import { onMount } from 'svelte'
  import { formatBytes } from '../../lib/format'
  import { parseComposeFiles, type DockerComposeFile } from '../../lib/docker'
  import { runDockerCommand } from '../../lib/docker-client'
  import ConfirmDialog from '../ui/ConfirmDialog.svelte'
  import Icon from '../ui/Icon.svelte'

  export let deviceId: string
  export let authHeaders: Record<string, string> = {}
  // A successful deploy can create containers, so the list above has to ask the
  // device again instead of waiting for the next sample.
  export let onChanged: () => void = () => {}

  let files: DockerComposeFile[] = []
  let loading = false
  let scanned = false
  let error = ''

  let selected: DockerComposeFile | null = null
  let draft = ''
  let saving = false
  let deploying = false
  let notice = ''
  let deployTarget: DockerComposeFile | null = null
  let deployOutput = ''
  let pendingSelect: DockerComposeFile | null = null

  $: dirty = selected != null && draft !== selected.content

  onMount(() => {
    void load()
  })

  async function load() {
    loading = true
    error = ''
    try {
      const result = await runDockerCommand(deviceId, 'compose_read', '', '', {
        headers: authHeaders,
        timeoutMs: 40_000
      })
      files = parseComposeFiles(result.message)
      // Keep the editor on the same file across a rescan, but drop a selection
      // whose file no longer exists rather than editing a path that is gone.
      if (selected) {
        selected = files.find((file) => file.path === selected!.path) || null
        if (!selected) draft = ''
      }
    } catch (e: any) {
      files = []
      error = e?.message || 'Could not read compose files from the device'
    } finally {
      loading = false
      scanned = true
    }
  }

  function select(file: DockerComposeFile) {
    if (dirty) {
      pendingSelect = file
      return
    }
    applySelect(file)
  }

  function applySelect(file: DockerComposeFile) {
    selected = file
    draft = file.content
    notice = ''
    deployOutput = ''
  }

  async function save() {
    if (!selected || saving) return
    saving = true
    error = ''
    notice = ''
    try {
      await runDockerCommand(deviceId, 'compose_write', selected.path, draft, {
        headers: authHeaders,
        timeoutMs: 40_000
      })
      selected = { ...selected, content: draft, size_bytes: draft.length }
      const index = files.findIndex((file) => file.path === selected!.path)
      if (index >= 0) files[index] = selected
      files = [...files]
      notice = 'Saved on the device'
    } catch (e: any) {
      error = e?.message || 'Could not save the compose file'
    } finally {
      saving = false
    }
  }

  async function deploy() {
    const target = deployTarget
    if (!target) return
    deploying = true
    error = ''
    notice = ''
    deployOutput = ''
    try {
      const result = await runDockerCommand(deviceId, 'compose_up', target.path, '', {
        headers: authHeaders,
        timeoutMs: 90_000
      })
      deployOutput = result.message || ''
      notice = `Deployed ${target.path.split('/').pop()}`
      onChanged()
      await load()
    } catch (e: any) {
      error = e?.message || 'Could not deploy the compose file'
    } finally {
      deploying = false
      deployTarget = null
    }
  }

  function baseName(path: string): string {
    return path.split('/').filter(Boolean).pop() || path
  }

  function directory(path: string): string {
    const parts = path.split('/')
    parts.pop()
    return parts.join('/') || '/'
  }
</script>

<article class="maintenance-card">
  <header class="card-head">
    <div>
      <h2>Compose files</h2>
      <p class="sub">Read, edit and deploy compose projects found on the device.</p>
    </div>
    <button class="btn-ghost" on:click={() => void load()} disabled={loading}>
      <Icon name="refresh" size={13} /> {loading ? 'Scanning…' : 'Scan'}
    </button>
  </header>

  {#if error}
    <div class="inline-error" role="alert">
      {error}
      <button on:click={() => void load()} disabled={loading}>Retry</button>
    </div>
  {/if}
  {#if notice}
    <p class="notice" role="status">{notice}</p>
  {/if}

  {#if loading && !scanned}
    <p class="subtle pad">Scanning the usual compose locations…</p>
  {:else if files.length === 0}
    <p class="subtle pad">
      No compose project was found. The agent looks in the usual locations, such as
      <span class="mono">/opt</span>, <span class="mono">/srv</span> and the service user's home.
    </p>
  {:else}
    <ul class="files">
      {#each files as file (file.path)}
        <li class:sel={selected?.path === file.path}>
          <button class="file" on:click={() => select(file)} title={file.path}>
            <span class="file-name mono">{baseName(file.path)}</span>
            <span class="file-dir">{directory(file.path)}</span>
            <span class="file-size">{formatBytes(file.size_bytes)}</span>
          </button>
          <button class="btn-ghost deploy" on:click={() => (deployTarget = file)} disabled={deploying}>
            Deploy
          </button>
        </li>
      {/each}
    </ul>
  {/if}

  {#if selected}
    <div class="editor">
      <div class="editor-head">
        <span class="editor-path mono" title={selected.path}>{selected.path}</span>
        {#if dirty}<span class="dirty">unsaved</span>{/if}
      </div>
      <textarea bind:value={draft} spellcheck="false" rows="14" aria-label="Compose file contents"></textarea>
      <div class="editor-actions">
        <span class="subtle">
          Saving writes the file on the device. Deploy runs <span class="mono">docker compose up -d</span>.
        </span>
        <button class="btn-primary" on:click={() => void save()} disabled={!dirty || saving}>
          {saving ? 'Saving…' : 'Save'}
        </button>
      </div>
    </div>
  {/if}

  {#if deployOutput}
    <pre class="output">{deployOutput}</pre>
  {/if}
</article>

<ConfirmDialog
  open={pendingSelect != null}
  title="Discard unsaved changes?"
  message="The editor has changes that were not written to the device. Opening another file will lose them."
  confirmLabel="Discard"
  destructive={true}
  on:confirm={() => {
    const next = pendingSelect
    pendingSelect = null
    if (next) applySelect(next)
  }}
  on:cancel={() => (pendingSelect = null)}
/>

<ConfirmDialog
  open={deployTarget != null}
  title="Deploy compose project?"
  message={`This runs docker compose up -d for ${deployTarget?.path || ''}. Containers defined in that file may be created or recreated.`}
  confirmLabel={deploying ? 'Deploying…' : 'Deploy'}
  destructive={false}
  busy={deploying}
  on:confirm={deploy}
  on:cancel={() => (deployTarget = null)}
/>

<style>
  .maintenance-card {
    display: grid;
    gap: 0;
    align-content: start;
    background: var(--card);
    border: 1px solid var(--border);
    border-radius: var(--radius);
    overflow: hidden;
    min-width: 0;
  }

  .card-head {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: 0.6rem;
    padding: 0.75rem 0.9rem;
    border-bottom: 1px solid var(--border);
  }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }
  .sub { margin: 0.2rem 0 0; color: var(--text-faint); font-size: 0.72rem; line-height: 1.4; }

  .subtle { color: var(--text-faint); font-size: 0.74rem; line-height: 1.5; }
  .pad { margin: 0; padding: 0.85rem 0.9rem; }

  .files { list-style: none; margin: 0; padding: 0; max-height: 17rem; overflow-y: auto; }
  .files li {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 0.4rem;
    border-bottom: 1px solid var(--border);
    padding-right: 0.6rem;
  }
  .files li:last-child { border-bottom: 0; }
  .files li.sel { background: var(--accent-dim); box-shadow: inset 2px 0 0 var(--accent); }

  .file {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    grid-template-areas: 'name size' 'dir size';
    gap: 0.1rem 0.6rem;
    padding: 0.5rem 0.9rem;
    border: 0;
    background: transparent;
    color: var(--text);
    text-align: left;
    cursor: pointer;
    min-width: 0;
  }
  .file:hover { background: var(--card-hover); }
  .file-name { grid-area: name; color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .file-dir { grid-area: dir; color: var(--text-faint); font-size: 0.68rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .file-size { grid-area: size; align-self: center; color: var(--text-muted); font-size: 0.7rem; font-variant-numeric: tabular-nums; }

  .editor { border-top: 1px solid var(--border); padding: 0.7rem 0.9rem 0.9rem; display: grid; gap: 0.5rem; }
  .editor-head { display: flex; align-items: center; gap: 0.5rem; flex-wrap: wrap; }
  .editor-path { color: var(--text); font-size: 0.7rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .dirty { color: var(--warn); font-size: 0.62rem; font-weight: 700; letter-spacing: 0.05em; text-transform: uppercase; }

  textarea {
    width: 100%;
    box-sizing: border-box;
    min-height: 12rem;
    resize: vertical;
    padding: 0.55rem 0.65rem;
    background: #08080a;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-family: var(--mono);
    font-size: 0.72rem;
    line-height: 1.5;
  }
  textarea:focus { outline: none; border-color: var(--accent-border); }

  .editor-actions { display: flex; align-items: center; justify-content: space-between; gap: 0.6rem; flex-wrap: wrap; }

  .inline-error {
    display: flex;
    align-items: center;
    gap: 0.6rem;
    margin: 0.7rem 0.9rem 0;
    padding: 0.5rem 0.65rem;
    border: 1px solid rgba(248, 113, 113, 0.35);
    border-radius: var(--radius-sm);
    background: var(--crit-dim);
    color: var(--crit);
    font-size: 0.74rem;
  }
  .inline-error button {
    margin-left: auto;
    border: 1px solid rgba(248, 113, 113, 0.4);
    background: transparent;
    color: var(--crit);
    border-radius: var(--radius-sm);
    padding: 0.15rem 0.5rem;
    font-size: 0.7rem;
    cursor: pointer;
  }

  .notice { margin: 0.7rem 0.9rem 0; color: var(--accent); font-size: 0.74rem; }

  .output {
    margin: 0.7rem 0.9rem 0.9rem;
    padding: 0.55rem 0.65rem;
    max-height: 14rem;
    overflow: auto;
    background: #08080a;
    border: 1px solid var(--border);
    border-radius: var(--radius-sm);
    color: var(--text);
    font-family: var(--mono);
    font-size: 0.7rem;
    white-space: pre-wrap;
  }

  .btn-ghost, .btn-primary {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.3rem 0.55rem;
    border: 1px solid var(--border-strong);
    border-radius: var(--radius-sm);
    background: transparent;
    color: var(--text);
    font-size: 0.72rem;
    font-weight: 600;
    cursor: pointer;
    white-space: nowrap;
  }
  .btn-ghost:hover:not(:disabled) { background: var(--card-hover); }
  .btn-ghost.deploy { color: var(--accent); border-color: var(--accent-border); }
  .btn-primary { background: var(--accent-dim); border-color: var(--accent-border); color: var(--accent); }
  button:disabled { opacity: 0.5; cursor: default; }
</style>
