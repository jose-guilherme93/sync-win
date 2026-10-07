// Canvas 2D and Chart.js do not understand CSS custom properties. Assigning
// "var(--series-1)" to ctx.strokeStyle is silently ignored and the previous
// value is kept — on a fresh context that is black, so every sparkline drew an
// invisible line on the dark card. Resolve a var() reference against the
// document root before handing a colour to any canvas API.

export function resolveCssColor(value: string, fallback = '#34d399'): string {
  if (!value) return fallback
  const match = /^var\(\s*(--[A-Za-z0-9_-]+)\s*(?:,\s*([^)]+))?\s*\)$/.exec(value.trim())
  if (!match) return value

  const inlineFallback = match[2]?.trim()
  if (typeof window === 'undefined' || typeof getComputedStyle !== 'function' || !document?.documentElement) {
    return inlineFallback || fallback
  }
  const resolved = getComputedStyle(document.documentElement).getPropertyValue(match[1]).trim()
  return resolved || inlineFallback || fallback
}
