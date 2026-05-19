import { NextResponse, type NextRequest } from 'next/server';

const SESSION_COOKIE = 'klib_session';

// Paths exempt from auth — login UI, login API, PWA assets, Next internals.
const PUBLIC_PATHS = [
  '/login',
  '/offline',
  '/api/auth/login',
  '/manifest.json',
  '/favicon.svg',
  '/apple-touch-icon.png',
  '/icons',
];

function isPublic(pathname: string): boolean {
  if (pathname === '/' ) return false;
  if (pathname.startsWith('/_next')) return true;
  if (pathname.startsWith('/sw.js') || pathname.startsWith('/workbox-')) return true;
  return PUBLIC_PATHS.some((p) => pathname === p || pathname.startsWith(p + '/'));
}

export function middleware(req: NextRequest): NextResponse {
  const { pathname } = req.nextUrl;
  if (isPublic(pathname)) return NextResponse.next();

  const session = req.cookies.get(SESSION_COOKIE)?.value;
  if (session) return NextResponse.next();

  // For API calls, return 401 instead of redirecting (clients shouldn't follow HTML).
  if (pathname.startsWith('/api/')) {
    return new NextResponse(JSON.stringify({ error: 'unauthenticated' }), {
      status: 401,
      headers: { 'content-type': 'application/json' },
    });
  }

  const url = req.nextUrl.clone();
  url.pathname = '/login';
  url.searchParams.set('next', pathname);
  return NextResponse.redirect(url);
}

export const config = {
  // Skip Next.js internals and static files.
  matcher: ['/((?!_next/static|_next/image|favicon.ico|icons/).*)'],
};
