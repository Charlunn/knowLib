// POST /api/auth/login — proxies TOTP to the backend, then sets the HttpOnly
// session cookie on success. Cookie value is the JWT returned by the API.

import { NextResponse, type NextRequest } from 'next/server';
import { z } from 'zod';
import { backendUrl } from '@/lib/backend';
import { SESSION_COOKIE, cookieOptions } from '@/lib/auth';

const Body = z.object({ totp: z.string().regex(/^\d{6}$/) });

interface LoginRes {
  token: string;
  expires_at?: string;
}

export async function POST(req: NextRequest): Promise<NextResponse> {
  let parsed: z.infer<typeof Body>;
  try {
    parsed = Body.parse(await req.json());
  } catch {
    return NextResponse.json({ error: 'invalid_body' }, { status: 400 });
  }

  const upstream = await fetch(backendUrl('/api/auth/login'), {
    method: 'POST',
    headers: { 'content-type': 'application/json' },
    body: JSON.stringify({ totp: parsed.totp }),
    cache: 'no-store',
  });

  if (!upstream.ok) {
    const text = await upstream.text().catch(() => '');
    return NextResponse.json(
      { error: upstream.status === 401 ? 'invalid_totp' : upstream.status === 429 ? 'rate_limited' : 'login_failed', detail: text },
      { status: upstream.status },
    );
  }

  const data = (await upstream.json()) as LoginRes;
  if (!data.token) {
    return NextResponse.json({ error: 'no_token' }, { status: 502 });
  }

  // Compute cookie max-age from expires_at when provided; otherwise default 24h.
  let maxAge = 60 * 60 * 24;
  if (data.expires_at) {
    const exp = Date.parse(data.expires_at);
    if (Number.isFinite(exp)) {
      maxAge = Math.max(60, Math.floor((exp - Date.now()) / 1000));
    }
  }

  const res = NextResponse.json({ ok: true });
  res.cookies.set(SESSION_COOKIE, data.token, cookieOptions(maxAge));
  return res;
}
