export type NotificationSound = 'beep' | 'chime' | 'pulse' | 'none'

export const NOTIFICATION_SOUNDS: { id: NotificationSound; label: string }[] = [
  { id: 'beep', label: 'Beep' },
  { id: 'chime', label: 'Chime' },
  { id: 'pulse', label: 'Pulse' },
  { id: 'none', label: 'Silent' }
]

// One AudioContext for the whole app. The previous toast implementation created
// a new context for every notification and never closed it; browsers also
// suspend a context created before a user gesture, so the first sound could be
// silently dropped.
let ctx: AudioContext | null = null

function audioContext(): AudioContext | null {
  if (typeof window === 'undefined') return null
  const Ctor = window.AudioContext ?? (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
  if (!Ctor) return null
  if (!ctx) ctx = new Ctor()
  return ctx
}

// unlockNotificationAudio is safe to call on any click/keypress. A no-op when
// the browser has no Web Audio support.
export function unlockNotificationAudio(): void {
  const context = audioContext()
  if (context?.state === 'suspended') {
    void context.resume().catch(() => {})
  }
}

function tone(context: AudioContext, frequency: number, start: number, duration: number, volume: number): void {
  const oscillator = context.createOscillator()
  const gain = context.createGain()
  oscillator.type = 'sine'
  oscillator.frequency.value = frequency

  // A gentle curve instead of a hard start/stop; 0.35 keeps the loudest
  // setting comfortable.
  const level = Math.max(0.0001, Math.min(1, volume) * 0.35)
  gain.gain.setValueAtTime(level, start)
  gain.gain.exponentialRampToValueAtTime(0.0001, start + duration)

  oscillator.connect(gain)
  gain.connect(context.destination)
  oscillator.start(start)
  oscillator.stop(start + duration + 0.02)
}

export function playNotificationSound(kind: NotificationSound, volume = 0.5): void {
  if (kind === 'none') return
  const context = audioContext()
  if (!context) return
  if (context.state === 'suspended') {
    void context.resume().catch(() => {})
  }

  const now = context.currentTime
  switch (kind) {
    case 'chime':
      tone(context, 880, now, 0.15, volume)
      tone(context, 1320, now + 0.15, 0.2, volume)
      break
    case 'pulse':
      tone(context, 520, now, 0.08, volume)
      tone(context, 520, now + 0.14, 0.08, volume)
      break
    default:
      tone(context, 660, now, 0.2, volume)
  }
}
