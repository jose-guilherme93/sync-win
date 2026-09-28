<script lang="ts">
  import { createEventDispatcher, onMount } from 'svelte'
  import { apiFetch, apiURL, serverBase } from '../lib/api'

  export let open = false
  export let authHeaders: Record<string, string> = {}

  const dispatch = createEventDispatcher()

  type ConfigResponse = {
    provider: string
    enabled: boolean
    events: string[]
    config: Record<string, string>
    configured: boolean
  }

  type ProviderInfo = {
    name: string
    label: string
    description: string
  }

  const ALL_EVENTS = [
    { id: 'device_offline', label: 'Device goes offline', icon: '🔴' },
    { id: 'device_online', label: 'Device comes back online', icon: '🟢' },
    { id: 'sync_error', label: 'Sync error', icon: '⚠️' },
    { id: 'device_enrolled', label: 'New device enrolled', icon: '➕' },
  ]

  let configs: ConfigResponse[] = []
  let providers: ProviderInfo[] = []
  let inboxEnabled = true
  let inboxSound = 'beep'
  let telegramToken = ''
  let telegramChatId = ''
  let telegramEnabled = false
  let telegramEvents: string[] = ['device_offline', 'sync_error']
  let webhookUrl = ''
  let webhookSecret = ''
  let webhookEnabled = false
  let webhookEvents: string[] = ['device_offline', 'sync_error']

  let saving = false
  let testing = ''
  let detecting = false
  let toastMessage = ''
  let toastKind: 'success' | 'error' = 'success'
  let toastTimer = 0

  function notify(message: string, kind: 'success' | 'error' = 'success') {
    toastMessage = message
    toastKind = kind
    window.clearTimeout(toastTimer)
    toastTimer = window.setTimeout(() => (toastMessage = ''), kind === 'error' ? 4200 : 2400)
  }

  function close() {
    dispatch('close')
  }

  async function loadConfig() {
    try {
      const [cfgRes, provRes] = await Promise.all([
        apiFetch(apiURL(`/api/notifications/config`), { headers: authHeaders }),
        apiFetch(apiURL(`/api/notifications/providers`), { headers: authHeaders }),
      ])
      if (cfgRes.ok) configs = await cfgRes.json()
      if (provRes.ok) providers = await provRes.json()
    } catch {}

    // Populate form from configs.
    for (const c of configs) {
      if (c.provider === 'web_inbox') {
        inboxEnabled = c.enabled
        inboxSound = c.config?.sound || 'beep'
      }
      if (c.provider === 'telegram') {
        telegramEnabled = c.enabled
        telegramEvents = c.events || telegramEvents
        telegramChatId = c.config?.chat_id || ''
        // bot_token is masked, don't overwrite
      }
      if (c.provider === 'webhook') {
        webhookEnabled = c.enabled
        webhookEvents = c.events || webhookEvents
        webhookUrl = c.config?.url || ''
        // secret is masked
      }
    }
  }

  async function saveInbox() {
    saving = true
    try {
      const res = await apiFetch(apiURL(`/api/notifications/config`), {
        method: 'PUT',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({
          provider: 'web_inbox',
          enabled: inboxEnabled,
          events: [],
          config: { sound: inboxSound },
        }),
      })
      if (!res.ok) throw new Error(`${res.status}`)
      notify('In-site alerts saved.')
    } catch (e) {
      notify(e instanceof Error ? e.message : 'Could not save', 'error')
    } finally {
      saving = false
    }
  }

  async function detectChatId() {
    if (!telegramToken || telegramToken.includes('*')) {
      notify('Enter your bot token first', 'error')
      return
    }
    detecting = true
    try {
      const res = await apiFetch(apiURL(`/api/notifications/telegram/detect-chat`), {
        method: 'POST',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ bot_token: telegramToken }),
      })
      const data = await res.json()
      if (res.ok && data.chat_id) {
        telegramChatId = data.chat_id
        notify('Chat ID detected: ' + data.chat_id)
      } else {
        notify(data.error || 'Could not detect chat ID. Send /start to your bot first.', 'error')
      }
    } catch (e) {
      notify('Failed to detect chat ID', 'error')
    } finally {
      detecting = false
    }
  }

  async function saveTelegram() {
    saving = true
    try {
      const body: Record<string, any> = {
        provider: 'telegram',
        enabled: telegramEnabled,
        events: telegramEvents,
        config: { chat_id: telegramChatId },
      }
      // Only include bot_token if it's not the masked placeholder
      if (telegramToken && !telegramToken.includes('*')) {
        body.config.bot_token = telegramToken
      }
      const res = await apiFetch(apiURL(`/api/notifications/config`), {
        method: 'PUT',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const payload = await res.json().catch(() => ({}))
        throw new Error(payload.error || `${res.status}`)
      }
      notify('Telegram config saved.')
    } catch (e) {
      notify(e instanceof Error ? e.message : 'Could not save', 'error')
    } finally {
      saving = false
    }
  }

  async function saveWebhook() {
    saving = true
    try {
      const body: Record<string, any> = {
        provider: 'webhook',
        enabled: webhookEnabled,
        events: webhookEvents,
        config: { url: webhookUrl },
      }
      if (webhookSecret && !webhookSecret.includes('*')) {
        body.config.secret = webhookSecret
      }
      const res = await apiFetch(apiURL(`/api/notifications/config`), {
        method: 'PUT',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const payload = await res.json().catch(() => ({}))
        throw new Error(payload.error || `${res.status}`)
      }
      notify('Webhook config saved.')
    } catch (e) {
      notify(e instanceof Error ? e.message : 'Could not save', 'error')
    } finally {
      saving = false
    }
  }

  async function testProvider(provider: string) {
    testing = provider
    try {
      const res = await apiFetch(apiURL(`/api/notifications/test`), {
        method: 'POST',
        headers: { ...authHeaders, 'Content-Type': 'application/json' },
        body: JSON.stringify({ provider }),
      })
      if (!res.ok) {
        const payload = await res.json().catch(() => ({}))
        throw new Error(payload.error || `${res.status}`)
      }
      notify(`Test message sent via ${provider}.`)
    } catch (e) {
      notify(e instanceof Error ? e.message : 'Test failed', 'error')
    } finally {
      testing = ''
    }
  }

  function playSound() {
    try {
      const ctx = new AudioContext()
      const osc = ctx.createOscillator()
      const gain = ctx.createGain()
      osc.connect(gain)
      gain.connect(ctx.destination)
      gain.gain.value = 0.3
      if (inboxSound === 'chime') {
        osc.frequency.value = 880
        osc.start()
        osc.stop(ctx.currentTime + 0.15)
        const osc2 = ctx.createOscillator()
        const gain2 = ctx.createGain()
        osc2.connect(gain2)
        gain2.connect(ctx.destination)
        gain2.gain.value = 0.3
        osc2.frequency.value = 1320
        osc2.start(ctx.currentTime + 0.15)
        osc2.stop(ctx.currentTime + 0.3)
      } else if (inboxSound === 'beep') {
        osc.frequency.value = 660
        osc.start()
        osc.stop(ctx.currentTime + 0.2)
      }
    } catch {}
  }

  function toggleEvent(list: string[], event: string) {
    if (list.includes(event)) {
      return list.filter((e) => e !== event)
    }
    return [...list, event]
  }

  $: if (open) {
    loadConfig()
  }

  $: if (!open) {
    window.clearTimeout(toastTimer)
  }
</script>

{#if open}
  <div class="modal-backdrop" role="presentation" on:click={close}>
    <div class="modal" role="dialog" aria-modal="true" aria-labelledby="notif-title" tabindex="-1" on:click|stopPropagation on:keydown|stopPropagation>
      {#if toastMessage}
        <div class="toast {toastKind}" role="status">{toastMessage}</div>
      {/if}

      <div class="modal-header">
        <div>
          <p class="eyebrow">Settings</p>
          <h2 id="notif-title">Notifications</h2>
        </div>
        <button class="secondary" on:click={close}>Close</button>
      </div>

      <!-- In-site alerts -->
      <section class="notif-section">
        <div class="section-header">
          <h3>In-site alerts</h3>
          <label class="toggle">
            <input type="checkbox" bind:checked={inboxEnabled} />
            <span class="toggle-slider"></span>
          </label>
        </div>
        <p class="section-desc">Show popups with sound directly in the dashboard. No external account needed.</p>
        <div class="sound-row">
          <span class="field-label">Sound</span>
          <div class="sound-options">
            {#each [{ id: 'beep', label: 'Beep' }, { id: 'chime', label: 'Chime' }, { id: 'none', label: 'Silent' }] as opt}
              <label class="radio-pill" class:selected={inboxSound === opt.id}>
                <input type="radio" bind:group={inboxSound} value={opt.id} />
                {opt.label}
              </label>
            {/each}
            <button class="inline" on:click={playSound} disabled={inboxSound === 'none'}>Test</button>
          </div>
        </div>
        <button class="primary sm" on:click={saveInbox} disabled={saving}>{saving ? 'Saving...' : 'Save'}</button>
      </section>

      <!-- Telegram -->
      <section class="notif-section">
        <div class="section-header">
          <h3>Telegram</h3>
          <label class="toggle">
            <input type="checkbox" bind:checked={telegramEnabled} />
            <span class="toggle-slider"></span>
          </label>
        </div>
        <p class="section-desc">
          Deliver alerts to a Telegram chat via a bot created with
          <a href="https://t.me/BotFather" target="_blank" rel="noopener">@BotFather</a>.
        </p>
        <div class="form-grid">
          <label>Bot Token
            <input type="password" bind:value={telegramToken} placeholder="123456:ABC-DEF..." autocomplete="off" />
          </label>
          <label>Chat ID
            <div class="input-with-btn">
              <input type="text" bind:value={telegramChatId} placeholder="e.g. 9999 or @channelname" />
              <button class="secondary sm" on:click={detectChatId} disabled={detecting} title="Auto-detect chat ID from bot token">
                {detecting ? '...' : 'Detect'}
              </button>
            </div>
          </label>
        </div>
        <div class="events-row">
          <span class="field-label">Events</span>
          <div class="event-chips">
            {#each ALL_EVENTS as ev}
              <label class="chip-toggle" class:selected={telegramEvents.includes(ev.id)}>
                <input type="checkbox" checked={telegramEvents.includes(ev.id)} on:change={() => (telegramEvents = toggleEvent(telegramEvents, ev.id))} />
                {ev.icon} {ev.label}
              </label>
            {/each}
          </div>
        </div>
        <div class="section-actions">
          <button class="primary sm" on:click={saveTelegram} disabled={saving}>{saving ? 'Saving...' : 'Save'}</button>
          <button class="secondary sm" on:click={() => testProvider('telegram')} disabled={testing === 'telegram'}>
            {testing === 'telegram' ? 'Sending...' : 'Send test'}
          </button>
        </div>
      </section>

      <!-- Webhook -->
      <section class="notif-section">
        <div class="section-header">
          <h3>Webhook</h3>
          <label class="toggle">
            <input type="checkbox" bind:checked={webhookEnabled} />
            <span class="toggle-slider"></span>
          </label>
        </div>
        <p class="section-desc">POST every event as JSON to any HTTP(S) endpoint (Discord, Slack, n8n, Home Assistant, ...).</p>
        <div class="form-grid">
          <label>URL
            <input type="url" bind:value={webhookUrl} placeholder="http://homeassistant.local:8123/api/notify" />
          </label>
          <label>Secret (optional, for HMAC-SHA256 signature)
            <input type="password" bind:value={webhookSecret} placeholder="Leave empty to skip signing" autocomplete="off" />
          </label>
        </div>
        <div class="events-row">
          <span class="field-label">Events</span>
          <div class="event-chips">
            {#each ALL_EVENTS as ev}
              <label class="chip-toggle" class:selected={webhookEvents.includes(ev.id)}>
                <input type="checkbox" checked={webhookEvents.includes(ev.id)} on:change={() => (webhookEvents = toggleEvent(webhookEvents, ev.id))} />
                {ev.icon} {ev.label}
              </label>
            {/each}
          </div>
        </div>
        <div class="section-actions">
          <button class="primary sm" on:click={saveWebhook} disabled={saving}>{saving ? 'Saving...' : 'Save'}</button>
          <button class="secondary sm" on:click={() => testProvider('webhook')} disabled={testing === 'webhook'}>
            {testing === 'webhook' ? 'Sending...' : 'Send test'}
          </button>
        </div>
      </section>
    </div>
  </div>
{/if}

<style>
  .modal-backdrop {
    position: fixed;
    inset: 0;
    z-index: 100;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(2, 6, 23, 0.7);
    backdrop-filter: blur(4px);
  }
  .modal {
    display: flex;
    flex-direction: column;
    gap: 0.75rem;
    padding: 1.2rem;
    width: min(640px, 95vw);
    max-height: 88vh;
    overflow-y: auto;
    border: 1px solid rgba(96, 165, 250, 0.3);
    border-radius: 14px;
    background: #0f172a;
    box-shadow: 0 24px 80px rgba(2, 6, 23, 0.5);
  }
  .modal-header {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
  }
  .modal-header h2 { margin: 0.15rem 0; font-size: 1.15rem; }
  .eyebrow { color: #64748b; font-size: 0.65rem; text-transform: uppercase; letter-spacing: 0.06em; margin: 0; }

  .notif-section {
    padding: 0.8rem;
    border: 1px solid rgba(148, 163, 184, 0.14);
    border-radius: 10px;
    background: rgba(2, 6, 23, 0.35);
    display: flex;
    flex-direction: column;
    gap: 0.5rem;
  }
  .section-header {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .section-header h3 { margin: 0; font-size: 0.9rem; color: #e2e8f0; }
  .section-desc { margin: 0; color: #94a3b8; font-size: 0.78rem; }
  .section-desc a { color: #60a5fa; }

  .sound-row, .events-row {
    display: flex;
    align-items: center;
    gap: 0.5rem;
    flex-wrap: wrap;
  }
  .field-label { color: #94a3b8; font-size: 0.75rem; white-space: nowrap; }
  .sound-options { display: flex; gap: 0.4rem; align-items: center; flex-wrap: wrap; }

  .radio-pill {
    display: inline-flex;
    align-items: center;
    gap: 0.3rem;
    padding: 0.25rem 0.6rem;
    border: 1px solid rgba(148, 163, 184, 0.2);
    border-radius: 999px;
    font-size: 0.75rem;
    color: #94a3b8;
    cursor: pointer;
    transition: all 0.15s;
  }
  .radio-pill input { display: none; }
  .radio-pill.selected { background: rgba(45, 212, 191, 0.15); border-color: rgba(45, 212, 191, 0.4); color: #ccfbf1; }

  .chip-toggle {
    display: inline-flex;
    align-items: center;
    gap: 0.25rem;
    padding: 0.2rem 0.55rem;
    border: 1px solid rgba(148, 163, 184, 0.18);
    border-radius: 999px;
    font-size: 0.7rem;
    color: #94a3b8;
    cursor: pointer;
    transition: all 0.15s;
  }
  .chip-toggle input { display: none; }
  .chip-toggle.selected { background: rgba(59, 130, 246, 0.15); border-color: rgba(59, 130, 246, 0.4); color: #93c5fd; }

  .event-chips { display: flex; gap: 0.35rem; flex-wrap: wrap; }

  .form-grid {
    display: grid;
    gap: 0.5rem;
  }
  .form-grid label {
    display: flex;
    flex-direction: column;
    gap: 0.2rem;
    color: #94a3b8;
    font-size: 0.72rem;
  }
  .form-grid input {
    padding: 0.45rem 0.6rem;
    background: #020617;
    border: 1px solid rgba(148, 163, 184, 0.2);
    border-radius: 7px;
    color: #e2e8f0;
    font-size: 0.82rem;
  }
  .form-grid input:focus { outline: none; border-color: #3b82f6; }

  .section-actions { display: flex; gap: 0.5rem; align-items: center; }

  .input-with-btn { display: flex; gap: 0.4rem; align-items: stretch; }
  .input-with-btn input { flex: 1; min-width: 0; }

  .toggle {
    position: relative;
    width: 36px;
    height: 20px;
    cursor: pointer;
  }
  .toggle input { display: none; }
  .toggle-slider {
    position: absolute;
    inset: 0;
    background: #334155;
    border-radius: 999px;
    transition: background 0.2s;
  }
  .toggle-slider::before {
    content: '';
    position: absolute;
    top: 2px;
    left: 2px;
    width: 16px;
    height: 16px;
    background: #94a3b8;
    border-radius: 50%;
    transition: transform 0.2s, background 0.2s;
  }
  .toggle input:checked + .toggle-slider { background: rgba(45, 212, 191, 0.4); }
  .toggle input:checked + .toggle-slider::before { transform: translateX(16px); background: #2dd4bf; }

  .sm { font-size: 0.78rem; padding: 0.35rem 0.8rem; }

  .primary {
    padding: 0.45rem 0.9rem;
    border: 1px solid rgba(45, 212, 191, 0.3);
    border-radius: 7px;
    background: rgba(13, 148, 136, 0.2);
    color: #ccfbf1;
    font-size: 0.82rem;
    cursor: pointer;
    transition: background 0.15s;
  }
  .primary:hover { background: rgba(13, 148, 136, 0.35); }
  .primary:disabled { opacity: 0.5; cursor: default; }

  .secondary {
    padding: 0.45rem 0.9rem;
    border: 1px solid rgba(148, 163, 184, 0.2);
    border-radius: 7px;
    background: transparent;
    color: #94a3b8;
    font-size: 0.82rem;
    cursor: pointer;
    transition: background 0.15s;
  }
  .secondary:hover { background: rgba(148, 163, 184, 0.08); color: #e2e8f0; }

  .inline {
    border: none;
    background: none;
    color: #60a5fa;
    font-size: 0.72rem;
    cursor: pointer;
    padding: 0;
  }
  .inline:hover { text-decoration: underline; }
  .inline:disabled { opacity: 0.4; cursor: default; }

  .toast {
    position: sticky;
    top: 0;
    z-index: 10;
    margin-bottom: 0.5rem;
    padding: 0.6rem 0.85rem;
    border: 1px solid rgba(45, 212, 191, 0.4);
    border-left-width: 3px;
    border-radius: 10px;
    background: #0f2f35;
    color: #ccfbf1;
    font-size: 0.8rem;
  }
  .toast.error { border-color: rgba(248, 113, 113, 0.45); background: #3b1219; color: #fecaca; }
</style>
