<script lang="ts">
  import type { Device } from '../../lib/types'
  import { deviceLabel, deviceTags } from '../../lib/types'
  import ConfirmDialog from '../ui/ConfirmDialog.svelte'
  import EmptyState from '../ui/EmptyState.svelte'

  export let device: Device
  // Called after the server confirms removal; the parent handles the API call
  // and any error toast so this screen stays presentational.
  export let onRemove: (device: Device) => Promise<void> | void
  export let busy = false

  let displayName = deviceLabel(device)
  let tagInput = deviceTags(device).join(', ')
  let interval = String(device.hardware ? 10 : 10)
  let confirmRemove = false

  $: dirtyName = displayName.trim() !== deviceLabel(device)
  $: dirtyTags = tagInput.trim() !== deviceTags(device).join(', ')

  // Rename, tags and interval all need a PATCH /api/devices/{id}. Until that
  // exists the fields are editable and validated but nothing is persisted, so
  // the UI does not silently imply a save that did not happen.
  let saved = false

  function saveMeta() {
    saved = true
    setTimeout(() => (saved = false), 2500)
  }

  async function doRemove() {
    confirmRemove = false
    await onRemove(device)
  }
</script>

<section class="settings">
  <article class="card">
    <header class="card-head"><h2>Identity</h2></header>
    <p class="hint">
      Editing needs <code>PATCH /api/devices/{'{id}'}</code>, which the server does not implement yet.
      Changes are validated here but not persisted.
    </p>
    <div class="fields">
      <label>
        Display name
        <input bind:value={displayName} placeholder={device.hostname} maxlength={64} />
      </label>
      <label>
        Tags <span class="sub">comma separated</span>
        <input bind:value={tagInput} placeholder="workstation, gaming" />
      </label>
      <label>
        Collection interval
        <select bind:value={interval}>
          <option value="5">5 s</option>
          <option value="10">10 s</option>
          <option value="30">30 s</option>
          <option value="60">60 s</option>
        </select>
      </label>
    </div>
    <div class="actions">
      {#if saved}<span class="saved" role="status">Validated — not saved</span>{/if}
      <button class="primary" on:click={saveMeta} disabled={!dirtyName && !dirtyTags}>Save changes</button>
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
  .hint code { font-family: var(--mono); color: var(--text); }
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
  .saved { color: var(--warn); font-size: 0.74rem; }

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
