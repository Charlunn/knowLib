'use client';

import * as React from 'react';
import { Textarea } from '@/components/ui/textarea';
import { Button } from '@/components/ui/button';
import { api, ApiError } from '@/lib/api';
import { enqueueCapture, flushQueue, listRecent, type QueuedCapture } from '@/lib/idb';
import { toast } from '@/components/ui/toaster';
import { cn } from '@/lib/utils';

// /capture is the PWA start_url. Anti-friction: textarea autofocuses, no chrome.
// Cmd/Ctrl+Enter submits. Offline writes queue in IndexedDB and flush on reconnect.
export default function CapturePage(): JSX.Element {
  const ref = React.useRef<HTMLTextAreaElement | null>(null);
  const [content, setContent] = React.useState('');
  const [busy, setBusy] = React.useState(false);
  const [recent, setRecent] = React.useState<QueuedCapture[]>([]);
  const [online, setOnline] = React.useState(true);
  const [pending, setPending] = React.useState(0);

  const refreshRecent = React.useCallback(async () => {
    try {
      const items = await listRecent(5);
      setRecent(items);
      setPending(items.filter((i) => i.status === 'pending' || i.status === 'failed').length);
    } catch {
      // IDB may be unavailable in some contexts (private mode, SSR shouldn't reach here)
    }
  }, []);

  const flush = React.useCallback(async () => {
    try {
      const { sent } = await flushQueue(async (c) => {
        await api.capture({ content: c, source: 'web' });
      });
      if (sent > 0) toast({ description: `已同步 ${sent} 条离线快捕` });
    } catch {
      // swallow; we'll retry next online event
    } finally {
      await refreshRecent();
    }
  }, [refreshRecent]);

  React.useEffect(() => {
    ref.current?.focus();
    setOnline(navigator.onLine);
    void refreshRecent();
    void flush();

    const onOnline = () => {
      setOnline(true);
      void flush();
    };
    const onOffline = () => setOnline(false);
    const onFocus = () => {
      if (navigator.onLine) void flush();
    };
    window.addEventListener('online', onOnline);
    window.addEventListener('offline', onOffline);
    window.addEventListener('focus', onFocus);
    return () => {
      window.removeEventListener('online', onOnline);
      window.removeEventListener('offline', onOffline);
      window.removeEventListener('focus', onFocus);
    };
  }, [flush, refreshRecent]);

  const submit = React.useCallback(async () => {
    const text = content.trim();
    if (!text || busy) return;
    setBusy(true);
    try {
      if (!navigator.onLine) {
        await enqueueCapture(text);
        toast({ description: '已离线缓存,联网后自动同步' });
      } else {
        try {
          await api.capture({ content: text, source: 'web' });
          toast({ description: '已保存' });
        } catch (e) {
          // Network or 5xx — queue and let the flush loop handle it.
          if (e instanceof ApiError && e.status >= 400 && e.status < 500 && e.status !== 408) {
            // Genuine client errors aren't queueable.
            throw e;
          }
          await enqueueCapture(text);
          toast({ description: '暂存到本地队列,稍后重试' });
        }
      }
      setContent('');
      await refreshRecent();
      ref.current?.focus();
    } catch (e) {
      const msg = e instanceof Error ? e.message : '保存失败';
      toast({ description: msg, variant: 'destructive' });
    } finally {
      setBusy(false);
    }
  }, [content, busy, refreshRecent]);

  const onKeyDown = (e: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
      e.preventDefault();
      void submit();
    }
  };

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-3 p-4">
      <Textarea
        ref={ref}
        value={content}
        onChange={(e) => setContent(e.target.value)}
        onKeyDown={onKeyDown}
        placeholder="随手记一笔…  (Ctrl/⌘+Enter 保存)"
        className="min-h-[50vh] flex-1 resize-none border-none bg-transparent text-base shadow-none focus-visible:ring-0"
        autoFocus
        spellCheck={false}
      />
      <div className="flex items-center justify-between text-xs text-muted-foreground">
        <div className="flex items-center gap-2">
          <span
            className={cn(
              'inline-block h-2 w-2 rounded-full',
              online ? 'bg-emerald-500' : 'bg-amber-500',
            )}
            aria-hidden
          />
          <span>{online ? '在线' : '离线'}</span>
          {pending > 0 ? <span>· 待同步 {pending}</span> : null}
        </div>
        <Button onClick={submit} disabled={busy || !content.trim()} size="sm">
          {busy ? '保存中…' : '保存'}
        </Button>
      </div>
      {recent.length > 0 ? (
        <div className="mt-2 space-y-1 text-sm">
          <p className="text-xs uppercase tracking-wide text-muted-foreground">最近 5 条</p>
          <ul className="space-y-1">
            {recent.map((r) => (
              <li
                key={r.id}
                className="flex items-baseline justify-between gap-3 truncate rounded border px-3 py-2 text-muted-foreground"
              >
                <span className="truncate">{r.content.split('\n')[0]?.slice(0, 80) ?? ''}</span>
                <span className="shrink-0 text-xs">
                  {r.status === 'sent' ? '已同步' : r.status === 'failed' ? '失败,会重试' : '排队中'}
                </span>
              </li>
            ))}
          </ul>
        </div>
      ) : null}
    </div>
  );
}
