// Generic /api/* proxy: forwards to the upstream knowlib-api with the JWT
// from the session cookie attached as a Bearer token.
//
// /api/auth/login is handled by its own route file (sets the cookie).
// Everything else routes through here.

import { NextResponse, type NextRequest } from 'next/server';
import { backendUrl } from '@/lib/backend';
import { SESSION_COOKIE } from '@/lib/auth';

const HOP_BY_HOP = new Set([
  'connection',
  'keep-alive',
  'proxy-authenticate',
  'proxy-authorization',
  'te',
  'trailers',
  'transfer-encoding',
  'upgrade',
  'host',
]);

async function proxy(req: NextRequest, params: { path: string[] }): Promise<NextResponse> {
  const segments = params.path ?? [];
  // Defense in depth: never let this generic route handle the login endpoint.
  if (segments[0] === 'auth' && segments[1] === 'login') {
    return NextResponse.json({ error: 'use_dedicated_route' }, { status: 404 });
  }

  const token = req.cookies.get(SESSION_COOKIE)?.value;
  const search = req.nextUrl.search;
  const target = backendUrl(`/api/${segments.join('/')}${search}`);

  const headers = new Headers();
  for (const [k, v] of req.headers.entries()) {
    if (HOP_BY_HOP.has(k.toLowerCase())) continue;
    if (k.toLowerCase() === 'cookie') continue; // upstream uses Bearer, not cookies
    headers.set(k, v);
  }
  if (token) headers.set('authorization', `Bearer ${token}`);

  const init: RequestInit = {
    method: req.method,
    headers,
    cache: 'no-store',
    redirect: 'manual',
  };

  if (req.method !== 'GET' && req.method !== 'HEAD') {
    init.body = await req.arrayBuffer();
  }

  const upstream = await fetch(target, init);

  // Stream body straight back. Strip hop-by-hop headers.
  const outHeaders = new Headers();
  upstream.headers.forEach((v, k) => {
    if (!HOP_BY_HOP.has(k.toLowerCase())) outHeaders.set(k, v);
  });
  return new NextResponse(upstream.body, {
    status: upstream.status,
    headers: outHeaders,
  });
}

export async function GET(req: NextRequest, ctx: { params: { path: string[] } }) {
  return proxy(req, ctx.params);
}
export async function POST(req: NextRequest, ctx: { params: { path: string[] } }) {
  return proxy(req, ctx.params);
}
export async function PUT(req: NextRequest, ctx: { params: { path: string[] } }) {
  return proxy(req, ctx.params);
}
export async function PATCH(req: NextRequest, ctx: { params: { path: string[] } }) {
  return proxy(req, ctx.params);
}
export async function DELETE(req: NextRequest, ctx: { params: { path: string[] } }) {
  return proxy(req, ctx.params);
}
