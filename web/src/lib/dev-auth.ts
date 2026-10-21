// Development convenience: the dev stack injects VITE_DEV_TOKEN and the server
// accepts it only when SYNCWIN_ENV=development. Exchanging it for a normal
// session cookie keeps the rest of the dashboard unchanged — the same cookie,
// CSRF and /api/auth/me flow as a password sign-in.
export const devToken = (import.meta.env.VITE_DEV_TOKEN as string | undefined)?.trim() || ''

export async function signInWithDevToken(token: string = devToken): Promise<boolean> {
  if (!token) return false
  try {
    const response = await fetch('/api/auth/dev', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ token }),
      credentials: 'include'
    })
    return response.ok
  } catch {
    // An unreachable server falls back to the sign-in screen; it must not stop
    // the dashboard from booting.
    return false
  }
}
