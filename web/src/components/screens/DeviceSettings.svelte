<script lang="ts">
  import { createEventDispatcher, onDestroy } from 'svelte'
  import type { Device } from '../../lib/types'
  import { deviceLabel, deviceTags } from '../../lib/types'
  import { apiFetch, apiURL } from '../../lib/api'
  import ConfirmDialog from '../ui/ConfirmDialog.svelte'

  export let device: Device
  export let authHeaders: Record<string, string> = {}
  // Called after the server confirms removal; the parent handles the API call
  // and any error toast so this screen stays presentational.
  export let onRemove: (device: Device) => Promise<void> | void
  export let busy = false

  const dispatch = createEventDispatcher()
  let displayName = device.display_name || device.hostname
  let tagInput = deviceTags(device).join(', ')
  let interval = String(device.collection_interval_seconds || 10)
  let savedName = displayName
  let savedTags = tagInput
  let savedInterval = interval
  let confirmRemove = false
  let loadingSettings = false
  let saving = false
  let settingsError = ''
  let saved = false
  let savedTimer: ReturnType<typeof setTimeout> | null = null
  let loadedDeviceId = ''
  let settingsRequest = 0

  $: dirtyName = displayName.trim() !== savedName
  $: dirtyTags = tagInput.trim() !== savedTags
  $: dirtyInterval = interval !== savedInterval

  async function loadSettings() {
    const request = ++settingsRequest
    const deviceId = device.id
    loadingSettings = true
    settingsError = ''
    saved = false
    try {
      const response = await apiFetch(apiURL(`/api/devices/${deviceId}/settings`), { headers: authHeaders })
      if (!response.ok) throw new Error(`Could not load settings (${response.status})`)
      const settings = await response.json()
      if (request !== settingsRequest) return
      displayName = settings.display_name || device.hostname
      tagInput = Array.isArray(settings.tags) ? settings.tags.join(', ') : ''
      interval = String(settings.collection_interval_seconds || 10)
      savedName = displayName
      savedTags = tagInput
      savedInterval = interval
    } catch (e) {
      if (request !== settingsRequest) return
      settingsError = e instanceof Error ? e.message : 'Could not load device settings'
    } finally {
      if (request === settingsRequest) loadingSettings = false
    }
  }

  async function saveMeta() {
    saving = true
    settingsError = ''
    saved = false
    try {
      const response = await apiFetch(apiURL(`/api/devices/${device.id}`), {
        method: 'PATCH',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({
          display_name: displayName.trim(),
          tags: tagInput.split(',').map((tag) => tag.trim()).filter(Boolean),
          collection_interval_seconds: Number(interval)
        })
      })
      if (!response.ok) {
        const payload = await response.json().catch(() => ({}))
        throw new Error(payload.error || `Could not save settings (${response.status})`)
      }
      const settings = await response.json()
      displayName = settings.display_name || device.hostname
      tagInput = Array.isArray(settings.tags) ? settings.tags.join(', ') : ''
      interval = String(settings.collection_interval_seconds)
      savedName = displayName
      savedTags = tagInput
      savedInterval = interval
      saved = true
      dispatch('saved', settings)
      if (savedTimer) clearTimeout(savedTimer)
      savedTimer = setTimeout(() => (saved = false), 2500)
    } catch (e) {
      settingsError = e instanceof Error ? e.message : 'Could not save device settings'
    } finally {
      saving = false
    }
  }

  async function doRemove() {
    confirmRemove = false
    await onRemove(device)
  }

  $: if (device.id && loadedDeviceId !== device.id) {
    loadedDeviceId = device.id
    void loadSettings()
  }
  onDestroy(() => { if (savedTimer) clearTimeout(savedTimer) })
</script>

<section class="settings">
  <article class="card">
    <header class="card-head"><h2>Identity</h2></header>
    <p class="hint">Settings are saved to the server. The collection interval applies to the agent's telemetry cycle.</p>
    {#if settingsError}<p class="error" role="alert">{settingsError}</p>{/if}
    <div class="fields">
      <label>
        Display name
        <input bind:value={displayName} placeholder={device.hostname} maxlength={64} disabled={loadingSettings || saving} />
      </label>
      <label>
        Tags <span class="sub">comma separated</span>
        <input bind:value={tagInput} placeholder="workstation, gaming" maxlength={800} disabled={loadingSettings || saving} />
      </label>
      <label>
        Collection interval
        <select bind:value={interval} disabled={loadingSettings || saving}>
          <option value="5">5 s</option>
          <option value="10">10 s</option>
          <option value="30">30 s</option>
          <option value="60">60 s</option>
        </select>
      </label>
    </div>
    <div class="actions">
      {#if saved}<span class="saved" role="status">Saved</span>{/if}
      <button class="primary" on:click={saveMeta} disabled={loadingSettings || saving || (!dirtyName && !dirtyTags && !dirtyInterval)}>
        {saving ? 'Saving…' : 'Save changes'}
      </button>
    </div>
  </article>

  <article class="card">
    <header class="card-head"><h2>Identification</h2></header>
    <dl class="facts">
      <div><dt>Device ID</dt><dd class="mono">{device.id}</dd></div>
      <div><dt>Hostname</dt><dd class="mono">{device.hostname}</dd></div>
      <div><dt>Owner</dt><dd class="mono">{device.user_id}</dd></div>
      {#if device.hardware?.agent_version}
        <div><dt>Agent</dt><dd class="mono">{device.hardware.agent_version}</dd></div>
      {/if}
      {#if device.hardware?.kernel_version}
        <div><dt>Kernel</dt><dd class="mono">{device.hardware.kernel_version}</dd></div>
      {/if}
      {#if device.hardware?.architecture}
        <div><dt>Architecture</dt><dd class="mono">{device.hardware.architecture}</dd></div>
      {/if}
    </dl>
  </article>

  <article class="card danger-zone">
    <header class="card-head"><h2>Remove device</h2></header>
    <p class="hint danger-text">
      Deletes the device record and every preference file and save stored for it on the server.
      The agent keeps running on the machine and will re-register on its next cycle unless it is
      uninstalled first.
    </p>
    <button class="danger" on:click={() => (confirmRemove = true)}>Remove device and data…</button>
  </article>
</section>

<ConfirmDialog
  open={confirmRemove}
  title="Remove this device?"
  message={`This permanently deletes ${deviceLabel(device)} and all synchronized data. Type the hostname to confirm.`}
  confirmLabel="Remove permanently"
  requireText={device.hostname}
  destructive
  {busy}
  on:confirm={doRemove}
  on:cancel={() => (confirmRemove = false)}
/>

<style>
  .settings { display: grid; gap: 0.85rem; }

  .card { background: var(--card); border: 1px solid var(--border); border-radius: var(--radius); padding: 0.9rem; }
  .card-head { margin-bottom: 0.6rem; }
  .card-head h2 { margin: 0; font-size: 0.82rem; font-weight: 600; color: var(--text); }

  .hint { margin: 0 0 0.75rem; color: var(--text-muted); font-size: 0.78rem; line-height: 1.5; }
  .error { margin: 0 0 0.75rem; color: var(--crit); font-size: 0.78rem; }
  .danger-text { color: var(--text-muted); }
  .sub { color: var(--text-faint); font-size: 0.7rem; font-weight: 400; }

  .fields { display: grid; grid-template-columns: repeat(auto-fit, minmax(220px, 1fr)); gap: 0.75rem; }
  .fields label { display: grid; gap: 0.3rem; color: var(--text-muted); font-size: 0.75rem; }
  .fields input, .fields select {
    padding: 0.45rem 0.6rem; background: var(--card-inset); border: 1px solid var(--border);
    border-radius: var(--radius-sm); color: var(--text); font: inherit; font-size: 0.8rem;
  }
  .fields input:focus, .fields select:focus { outline: none; border-color: var(--accent-border); }

  .actions { display: flex; align-items: center; justify-content: flex-end; gap: 0.75rem; margin-top: 0.85rem; }
  .saved { color: var(--accent); font-size: 0.74rem; }

  button { padding: 0.45rem 0.85rem; border-radius: var(--radius-sm); font-weight: 600; font-size: 0.78rem; cursor: pointer; }
  button:disabled { opacity: 0.5; cursor: default; }
  .primary { border: 1px solid var(--accent-border); background: var(--accent-dim); color: var(--accent); }
  .primary:hover:not(:disabled) { background: rgba(52, 211, 153, 0.25); }
  .danger { border: 1px solid rgba(248, 113, 113, 0.45); background: var(--crit-dim); color: var(--crit); }
  .danger:hover { background: rgba(248, 113, 113, 0.25); }

  .danger-zone { border-color: rgba(248, 113, 113, 0.3); }

  .facts { display: grid; grid-template-columns: repeat(auto-fit, minmax(180px, 1fr)); gap: 0.6rem; margin: 0; }
  .facts div { display: grid; gap: 0.15rem; min-width: 0; }
  dt { color: var(--text-faint); font-size: 0.68rem; text-transform: uppercase; letter-spacing: 0.06em; }
  dd { margin: 0; color: var(--text); font-size: 0.78rem; overflow: hidden; text-overflow: ellipsis; }
</style>
