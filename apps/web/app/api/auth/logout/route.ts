// POST /api/auth/logout — clears the session cookie.
// Sessions are stateless JWTs; there is no server-side state to revoke.
// The effective logout is simply removing the HttpOnly cookie.

import { NextResponse } from 'next/server';
import { SESSION_COOKIE } from '@/lib/auth';

export async function POST(): Promise<NextResponse> {
  const res = NextResponse.json({ ok: true });
  // Clear the session cookie by setting it expired.
  res.cookies.set(SESSION_COOKIE, '', {
    httpOnly: true,
    sameSite: 'lax',
    secure: process.env.NODE_ENV === 'production',
    path: '/',
    maxAge: 0,
  });
  return res;
}
