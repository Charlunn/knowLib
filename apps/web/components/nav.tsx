'use client';

import Link from 'next/link';
import { usePathname } from 'next/navigation';
import { cn } from '@/lib/utils';

const items: { href: string; label: string }[] = [
  { href: '/capture', label: '快捕' },
  { href: '/inbox', label: 'Inbox' },
  { href: '/notes', label: '笔记' },
  { href: '/settings', label: '设置' },
];

export function Nav(): JSX.Element {
  const pathname = usePathname();
  // Hide chrome on /capture for that anti-friction notepad feel.
  if (pathname === '/capture') return <></>;
  return (
    <nav className="border-b">
      <div className="container flex h-12 items-center gap-4 text-sm">
        <Link href="/capture" className="font-semibold tracking-tight">
          knowLib
        </Link>
        <div className="flex items-center gap-3 text-muted-foreground">
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
      </div>
    </nav>
  );
}
