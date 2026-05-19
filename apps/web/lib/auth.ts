// Server-side helpers for the JWT session cookie.
// We keep the cookie HttpOnly and let the API issue / verify JWTs;
// the web layer just relays it.

import { cookies } from 'next/headers';

export const SESSION_COOKIE = 'klib_session';

export interface SessionInfo {
  token: string;
}

export function getSession(): SessionInfo | null {
  const c = cookies().get(SESSION_COOKIE);
  if (!c?.value) return null;
  return { token: c.value };
}

export function isAuthed(): boolean {
  return getSession() !== null;
}

// Cookie options used both by the login route and by middleware-issued redirects.
// `secure` is enabled outside development so dev on localhost still works over http.
export function cookieOptions(maxAgeSec: number) {
  return {
    httpOnly: true,
    sameSite: 'lax' as const,
    secure: process.env.NODE_ENV === 'production',
    path: '/',
    maxAge: maxAgeSec,
  };
}
