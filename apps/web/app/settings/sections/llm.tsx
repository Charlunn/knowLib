'use client';

import * as React from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { api } from '@/lib/api';
import { toast } from '@/components/ui/toaster';
import type { Settings } from '@/lib/types';

const Schema = z.object({
  base_url: z.string().url('需要合法 URL'),
  model: z.string().min(1),
  api_key: z.string().optional(),
});
type FormVals = z.infer<typeof Schema>;

export function LlmSection({ settings, onSaved }: { settings: Settings; onSaved: () => Promise<void> }): JSX.Element {
  const [testing, setTesting] = React.useState(false);
  const [testOut, setTestOut] = React.useState<string | null>(null);
  const apiKeySet = settings.llm?.api_key_set ?? false;
  const { register, handleSubmit, formState: { errors, isSubmitting } } = useForm<FormVals>({
    resolver: zodResolver(Schema),
    defaultValues: {
      base_url: settings.llm?.base_url ?? '',
      model: settings.llm?.model ?? '',
      api_key: '',
    },
  });

  const onSubmit = handleSubmit(async (vals) => {
    const update: { base_url?: string; model?: string; api_key?: string } = {
      base_url: vals.base_url,
      model: vals.model,
    };
    if (vals.api_key && vals.api_key.length > 0) update.api_key = vals.api_key;
    try {
      await api.updateSettings({ llm: update });
      toast({ description: '已保存' });
      await onSaved();
    } catch (e) {
      toast({ description: e instanceof Error ? e.message : '保存失败', variant: 'destructive' });
    }
  });

  const test = async () => {
    setTesting(true);
    setTestOut(null);
    try {
      const r = await api.testLlm('say "ok"');
      setTestOut(r.output);
    } catch (e) {
      setTestOut(e instanceof Error ? e.message : '测试失败');
    } finally {
      setTesting(false);
    }
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle>LLM Provider</CardTitle>
      </CardHeader>
      <CardContent>
        <form className="space-y-4" onSubmit={onSubmit}>
          <div className="space-y-1.5">
            <Label htmlFor="llm-base">Base URL</Label>
            <Input id="llm-base" {...register('base_url')} placeholder="https://api.deepseek.com/v1" />
            {errors.base_url ? <p className="text-xs text-destructive">{errors.base_url.message}</p> : null}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="llm-model">Model</Label>
            <Input id="llm-model" {...register('model')} placeholder="deepseek-chat" />
            {errors.model ? <p className="text-xs text-destructive">{errors.model.message}</p> : null}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="llm-key">API Key</Label>
            <Input id="llm-key" type="password" {...register('api_key')} placeholder={apiKeySet ? '已设置(留空保持不变)' : 'sk-...'} />
          </div>
          <div className="flex gap-2">
            <Button type="submit" disabled={isSubmitting}>保存</Button>
            <Button type="button" variant="outline" onClick={test} disabled={testing}>
              {testing ? '测试中…' : '测试连接'}
            </Button>
          </div>
          {testOut ? (
            <pre className="overflow-x-auto rounded bg-muted p-2 text-xs">{testOut}</pre>
          ) : null}
        </form>
      </CardContent>
    </Card>
  );
}
