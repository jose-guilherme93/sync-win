<script lang="ts">
  import { createEventDispatcher, onMount, onDestroy } from 'svelte'

  export let event: {
    id: number
    type: string
    device_id: string
    hostname: string
    message: string
    created_at: string
  }

  export let sound: string = 'beep'

  const dispatch = createEventDispatcher()

  const AUTO_DISMISS_MS = event.type === 'sync_error' ? 15000 : 8000
  let remaining = AUTO_DISMISS_MS
  let paused = false
  let timer: ReturnType<typeof setInterval> | null = null

  function startTimer() {
    remaining = AUTO_DISMISS_MS
    timer = setInterval(() => {
      if (paused) return
      remaining -= 100
      if (remaining <= 0) {
        dismiss()
      }
    }, 100)
  }

  onMount(() => startTimer())

  onDestroy(() => {
    if (timer) clearInterval(timer)
  })

  function icon(type: string) {
    switch (type) {
      case 'device_offline': return '🔴'
      case 'device_online': return '🟢'
      case 'sync_error': return '⚠️'
      case 'device_enrolled': return '➕'
      default: return 'ℹ️'
    }
  }

  function timeAgo(ts: string) {
    const t = Date.parse(ts)
    if (!Number.isFinite(t)) return ''
    const diff = Math.max(0, Math.floor((Date.now() - t) / 1000))
    if (diff < 10) return 'just now'
    if (diff < 60) return `${diff}s ago`
    if (diff < 3600) return `${Math.floor(diff / 60)}m ago`
    return `${Math.floor(diff / 3600)}h ago`
  }

  function dismiss() {
    if (timer) clearInterval(timer)
    dispatch('dismiss', { id: event.id })
  }

  function playAlert(kind: string) {
    if (kind === 'none') return
    try {
      const ctx = new AudioContext()
      if (kind === 'chime') {
        const osc = ctx.createOscillator()
        const g = ctx.createGain()
        osc.connect(g); g.connect(ctx.destination)
        g.gain.value = 0.3
        osc.frequency.value = 880
        osc.start(); osc.stop(ctx.currentTime + 0.15)
        const osc2 = ctx.createOscillator()
        const g2 = ctx.createGain()
        osc2.connect(g2); g2.connect(ctx.destination)
        g2.gain.value = 0.3
        osc2.frequency.value = 1320
        osc2.start(ctx.currentTime + 0.15)
        osc2.stop(ctx.currentTime + 0.3)
      } else {
        const osc = ctx.createOscillator()
        const g = ctx.createGain()
        osc.connect(g); g.connect(ctx.destination)
        g.gain.value = 0.3
        osc.frequency.value = 660
        osc.start(); osc.stop(ctx.currentTime + 0.2)
      }
    } catch {}
  }

  $: if (event) playAlert(sound)
  $: progress = remaining / AUTO_DISMISS_MS
</script>

<!-- svelte-ignore a11y-no-static-element-interactions -->
<div
  class="toast"
  class:error={event.type === 'sync_error'}
  class:warn={event.type === 'device_offline'}
  class:good={event.type === 'device_online'}
  role="alert"
  on:mouseenter={() => (paused = true)}
  on:mouseleave={() => (paused = false)}
>
  <span class="toast-icon">{icon(event.type)}</span>
  <div class="toast-body">
    <strong>{event.hostname || event.device_id}</strong>
    <span>{event.message}</span>
  </div>
  <span class="toast-time">{timeAgo(event.created_at)}</span>
  <button class="toast-close" on:click={dismiss} title="Dismiss">✕</button>
  <div class="toast-progress" style="width: {progress * 100}%"></div>
</div>

<style>
  .toast {
    display: flex;
    align-items: flex-start;
    gap: 0.5rem;
    padding: 0.55rem 0.7rem;
    border: 1px solid rgba(148, 163, 184, 0.15);
    border-left-width: 3px;
    border-radius: 9px;
    background: rgba(15, 23, 42, 0.95);
    backdrop-filter: blur(8px);
    min-width: 260px;
    max-width: 380px;
    animation: slideIn 0.25s ease-out;
    position: relative;
    overflow: hidden;
  }
  .toast.error { border-left-color: #f87171; background: rgba(30, 10, 12, 0.95); }
  .toast.warn { border-left-color: #fbbf24; }
  .toast.good { border-left-color: #34d399; }

  .toast-icon { font-size: 1rem; flex-shrink: 0; margin-top: 0.1rem; }
  .toast-body {
    flex: 1;
    display: flex;
    flex-direction: column;
    gap: 0.1rem;
    min-width: 0;
  }
  .toast-body strong { font-size: 0.78rem; color: #f8fafc; }
  .toast-body span { font-size: 0.72rem; color: #94a3b8; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .toast-time { font-size: 0.6rem; color: #64748b; white-space: nowrap; flex-shrink: 0; }
  .toast-close {
    border: none;
    background: none;
    color: #64748b;
    font-size: 0.7rem;
    cursor: pointer;
    padding: 0.1rem 0.2rem;
    border-radius: 4px;
    line-height: 1;
    flex-shrink: 0;
    transition: color 0.15s;
  }
  .toast-close:hover { color: #e2e8f0; }

  .toast-progress {
    position: absolute;
    bottom: 0;
    left: 0;
    height: 2px;
    background: rgba(148, 163, 184, 0.4);
    transition: width 0.1s linear;
    border-radius: 0 0 9px 0;
  }
  .toast.error .toast-progress { background: rgba(248, 113, 113, 0.5); }
  .toast.warn .toast-progress { background: rgba(251, 191, 36, 0.5); }
  .toast.good .toast-progress { background: rgba(52, 211, 153, 0.5); }

  @keyframes slideIn {
    from { transform: translateX(100%); opacity: 0; }
    to { transform: translateX(0); opacity: 1; }
  }
</style>
