'use client';

import Link from 'next/link';
import { usePathname, useRouter } from 'next/navigation';
import * as React from 'react';
import { cn } from '@/lib/utils';
import { api } from '@/lib/api';

const items: { href: string; label: string }[] = [
  { href: '/capture', label: '快捕' },
  { href: '/inbox', label: 'Inbox' },
  { href: '/notes', label: '笔记' },
  { href: '/settings', label: '设置' },
];

export function Nav(): JSX.Element {
  const pathname = usePathname();
  const router = useRouter();
  const [loggingOut, setLoggingOut] = React.useState(false);

  // Hide chrome on /capture for that anti-friction notepad feel.
  // 注：用户反馈用起来麻烦，改为始终显示导航栏。
  // if (pathname === '/capture') return <></>;

  const onLogout = async () => {
    if (loggingOut) return;
    setLoggingOut(true);
    try {
      await api.logout();
    } catch {
      // Best-effort: even if the API call fails, the cookie is still server-side.
      // Force the redirect either way.
    }
    router.replace('/login');
    router.refresh();
  };

  return (
    <nav className="border-b">
      <div className="container flex h-12 items-center gap-4 text-sm">
        <Link href="/capture" className="font-semibold tracking-tight">
          knowLib
        </Link>
        <div className="flex flex-1 items-center gap-3 text-muted-foreground">
          {items.map((it) => {
            const active = pathname?.startsWith(it.href);
            return (
              <Link
                key={it.href}
                href={it.href}
                className={cn('px-2 py-1 rounded-md hover:text-foreground', active && 'text-foreground bg-accent')}
              >
                {it.label}
              </Link>
            );
          })}
        </div>
        <button
          onClick={onLogout}
          disabled={loggingOut}
          className="px-2 py-1 rounded-md text-xs text-muted-foreground hover:text-foreground disabled:opacity-50"
          aria-label="退出登录"
        >
          {loggingOut ? '退出中…' : '退出'}
        </button>
      </div>
    </nav>
  );
}
