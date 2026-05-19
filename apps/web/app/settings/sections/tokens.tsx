'use client';

import * as React from 'react';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { api } from '@/lib/api';
import { toast } from '@/components/ui/toaster';
import type { ApiToken } from '@/lib/types';

export function TokensSection(): JSX.Element {
  const [tokens, setTokens] = React.useState<ApiToken[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [name, setName] = React.useState('');
  const [creating, setCreating] = React.useState(false);
  const [issued, setIssued] = React.useState<{ token: string; meta: ApiToken } | null>(null);

  const reload = React.useCallback(async () => {
    setLoading(true);
    try {
      setTokens(await api.listTokens());
    } catch (e) {
      toast({ description: e instanceof Error ? e.message : '加载失败', variant: 'destructive' });
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void reload();
  }, [reload]);

  const create = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!name.trim()) return;
    setCreating(true);
    try {
      const r = await api.createToken(name.trim());
      setIssued(r);
      setName('');
      await reload();
    } catch (e) {
      toast({ description: e instanceof Error ? e.message : '创建失败', variant: 'destructive' });
    } finally {
      setCreating(false);
    }
  };

  const revoke = async (id: string) => {
    if (!confirm('吊销这个 token?')) return;
    try {
      await api.revokeToken(id);
      await reload();
      toast({ description: '已吊销' });
    } catch (e) {
      toast({ description: e instanceof Error ? e.message : '吊销失败', variant: 'destructive' });
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>API Tokens</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <form className="flex gap-2" onSubmit={create}>
          <div className="flex-1">
            <Label htmlFor="tk-name" className="sr-only">名称</Label>
            <Input
              id="tk-name"
              placeholder="名称(如 claude-desktop)"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <Button type="submit" disabled={creating || !name.trim()}>新建</Button>
        </form>

        {loading ? (
          <p className="text-sm text-muted-foreground">加载中…</p>
        ) : tokens.length === 0 ? (
          <p className="text-sm text-muted-foreground">还没有 token。</p>
        ) : (
          <ul className="divide-y rounded border">
            {tokens.map((t) => (
              <li key={t.id} className="flex items-center justify-between p-3 text-sm">
                <div>
                  <div className="font-medium">{t.name}</div>
                  <div className="text-xs text-muted-foreground">
                    创建于 {new Date(t.created_at).toLocaleString()}
                    {t.last_used_at ? ` · 最近使用 ${new Date(t.last_used_at).toLocaleString()}` : ' · 未使用'}
                  </div>
                </div>
                <Button size="sm" variant="destructive" onClick={() => revoke(t.id)}>
                  吊销
                </Button>
              </li>
            ))}
          </ul>
        )}

        <Dialog open={!!issued} onOpenChange={(o) => { if (!o) setIssued(null); }}>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>新 Token 已生成</DialogTitle>
              <DialogDescription>
                这是你唯一一次看到完整 token 的机会,请立刻复制并保存。
              </DialogDescription>
            </DialogHeader>
            <pre className="overflow-x-auto rounded border bg-muted p-3 text-xs">{issued?.token ?? ''}</pre>
            <div className="flex justify-end gap-2">
              <Button
                variant="outline"
                onClick={async () => {
                  if (issued) {
                    try {
                      await navigator.clipboard.writeText(issued.token);
                      toast({ description: '已复制' });
                    } catch {
                      toast({ description: '复制失败', variant: 'destructive' });
                    }
                  }
                }}
              >
                复制
              </Button>
              <Button onClick={() => setIssued(null)}>已保存</Button>
            </div>
          </DialogContent>
        </Dialog>
      </CardContent>
    </Card>
  );
}
