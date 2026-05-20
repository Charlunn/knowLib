'use client';

import * as React from 'react';
import { Button } from '@/components/ui/button';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { api, ApiError } from '@/lib/api';
import { toast } from '@/components/ui/toaster';
import type { Note, TidyResultItem } from '@/lib/types';

interface ResultsByPath {
  [sourcePath: string]: TidyResultItem;
}

export default function InboxPage(): JSX.Element {
  const [items, setItems] = React.useState<Note[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [busyPaths, setBusyPaths] = React.useState<Set<string>>(new Set());
  const [allBusy, setAllBusy] = React.useState(false);
  const [results, setResults] = React.useState<ResultsByPath>({});

  const refresh = React.useCallback(async () => {
    setLoading(true);
    try {
      const list = await api.inbox();
      setItems(Array.isArray(list) ? list : []);
    } catch (e) {
      if (e instanceof ApiError) {
        toast({ description: `加载失败: ${e.message}`, variant: 'destructive' });
      }
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void refresh();
  }, [refresh]);

  const tidyOne = async (path: string) => {
    setBusyPaths((s) => new Set(s).add(path));
    try {
      const res = await api.tidy({ paths: [path] });
      const next: ResultsByPath = { ...results };
      for (const it of res.items ?? []) next[it.source_path] = it;
      setResults(next);
      toast({ description: '已整理' });
      await refresh();
    } catch (e) {
      const msg = e instanceof Error ? e.message : '整理失败';
      toast({ description: msg, variant: 'destructive' });
    } finally {
      setBusyPaths((s) => {
        const next = new Set(s);
        next.delete(path);
        return next;
      });
    }
  };

  const tidyAll = async () => {
    setAllBusy(true);
    try {
      const res = await api.tidy({ all: true });
      const next: ResultsByPath = { ...results };
      for (const it of res.items ?? []) next[it.source_path] = it;
      setResults(next);
      toast({ description: `已整理 ${res.items?.length ?? 0} 条` });
      await refresh();
    } catch (e) {
      const msg = e instanceof Error ? e.message : '整理失败';
      toast({ description: msg, variant: 'destructive' });
    } finally {
      setAllBusy(false);
    }
  };

  return (
    <div className="container max-w-3xl py-6">
      <div className="mb-4 flex items-center justify-between">
        <h1 className="text-xl font-semibold">Inbox ({items.length})</h1>
        <div className="flex gap-2">
          <Button variant="outline" size="sm" onClick={refresh} disabled={loading}>
            刷新
          </Button>
          <Button onClick={tidyAll} disabled={allBusy || items.length === 0} size="sm">
            {allBusy ? '整理中…' : '整理全部'}
          </Button>
        </div>
      </div>

      {loading ? (
        <p className="text-sm text-muted-foreground">加载中…</p>
      ) : items.length === 0 ? (
        <p className="text-sm text-muted-foreground">Inbox 为空。</p>
      ) : (
        <ul className="space-y-3">
          {items.map((it) => {
            const r = results[it.path];
            const busy = busyPaths.has(it.path);
            return (
              <li key={it.path}>
                <Card>
                  <CardHeader>
                    <CardTitle className="text-base">{it.title || it.path}</CardTitle>
                    <p className="text-xs text-muted-foreground">{it.path}</p>
                  </CardHeader>
                  <CardContent className="space-y-3">
                    {r ? (
                      <div className="rounded-md border bg-muted/30 p-2 text-sm">
                        <div>
                          状态:{' '}
                          <span
                            className={
                              r.status === 'ok'
                                ? 'text-emerald-600'
                                : r.status === 'partial'
                                  ? 'text-amber-600'
                                  : 'text-destructive'
                            }
                          >
                            {r.status}
                          </span>
                        </div>
                        {r.target_path ? <div>目标: {r.target_path}</div> : null}
                        {r.reason ? <div className="text-muted-foreground">{r.reason}</div> : null}
                      </div>
                    ) : null}
                    <div className="flex gap-2">
                      <Button size="sm" onClick={() => tidyOne(it.path)} disabled={busy || allBusy}>
                        {busy ? '整理中…' : '整理这篇'}
                      </Button>
                      <Button size="sm" variant="outline" onClick={refresh} disabled={busy || allBusy}>
                        重新加载
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
