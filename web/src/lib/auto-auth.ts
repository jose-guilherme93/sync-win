// The dev stack injects the dashboard token at build time so nobody has to paste
// it on every restart. It is the same credential the sign-in screen asks for —
// there is no separate development back door on the server.
export const autoToken = (import.meta.env.VITE_ACCESS_TOKEN as string | undefined)?.trim() || ''
