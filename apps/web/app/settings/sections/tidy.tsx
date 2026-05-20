'use client';

import * as React from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from '@/components/ui/card';
import { Switch } from '@/components/ui/switch';
import { api } from '@/lib/api';
import { toast } from '@/components/ui/toaster';
import type { Settings } from '@/lib/types';

const Schema = z.object({
  top_k: z.coerce.number().int().min(1).max(64),
  max_tokens: z.coerce.number().int().min(256).max(32768),
  prompt: z.string().min(1),
  auto_enabled: z.boolean(),
  auto_threshold: z.coerce.number().int().min(0).max(10000),
  auto_cron: z.string(),
});
type FormVals = z.infer<typeof Schema>;

export function TidySection({ settings, onSaved }: { settings: Settings; onSaved: () => Promise<void> }): JSX.Element {
  const { register, handleSubmit, watch, setValue, formState: { errors, isSubmitting } } = useForm<FormVals>({
    resolver: zodResolver(Schema),
    defaultValues: {
      top_k: settings.tidy?.top_k ?? 8,
      max_tokens: settings.tidy?.max_tokens ?? 4096,
      prompt: settings.tidy?.prompt ?? '',
      auto_enabled: settings.auto_tidy?.enabled ?? false,
      auto_threshold: settings.auto_tidy?.threshold ?? 5,
      auto_cron: settings.auto_tidy?.cron_spec ?? '',
    },
  });

  const autoEnabled = watch('auto_enabled');

  const onSubmit = handleSubmit(async (vals) => {
    try {
      await api.updateSettings({
        tidy: {
          top_k: vals.top_k,
          max_tokens: vals.max_tokens,
          prompt: vals.prompt,
        },
        auto_tidy: {
          enabled: vals.auto_enabled,
          threshold: vals.auto_threshold,
          cron_spec: vals.auto_cron,
        },
      });
      toast({ description: '已保存' });
      await onSaved();
    } catch (e) {
      toast({ description: e instanceof Error ? e.message : '保存失败', variant: 'destructive' });
    }
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>整理</CardTitle>
      </CardHeader>
      <CardContent>
        <form className="space-y-6" onSubmit={onSubmit}>
          {/* Tidy params */}
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <Label htmlFor="tidy-topk">Top-K (检索相关笔记数量)</Label>
              <Input id="tidy-topk" type="number" {...register('top_k')} />
              {errors.top_k ? <p className="text-xs text-destructive">{errors.top_k.message}</p> : null}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="tidy-mt">Max Tokens (LLM 输出上限)</Label>
              <Input id="tidy-mt" type="number" {...register('max_tokens')} />
              {errors.max_tokens ? <p className="text-xs text-destructive">{errors.max_tokens.message}</p> : null}
            </div>
          </div>

          {/* Auto tidy */}
          <div className="space-y-3 rounded border p-4">
            <div className="flex items-center justify-between">
              <div>
                <Label className="text-base">自动整理</Label>
                <p className="text-xs text-muted-foreground">
                  达到阈值或定时时间时自动跑一次「整理全部」
                </p>
              </div>
              <Switch
                checked={autoEnabled}
                onCheckedChange={(checked) => setValue('auto_enabled', checked)}
              />
            </div>

            <div className={autoEnabled ? '' : 'pointer-events-none opacity-50'}>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1.5">
                  <Label htmlFor="auto-threshold">阈值 (inbox ≥ N 个文件时触发)</Label>
                  <Input id="auto-threshold" type="number" {...register('auto_threshold')} />
                  <p className="text-xs text-muted-foreground">填 0 关闭阈值触发</p>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="auto-cron">定时 (cron 格式)</Label>
                  <Input
                    id="auto-cron"
                    placeholder="0 2 * * *  (每天 02:00)"
                    {...register('auto_cron')}
                  />
                  <p className="text-xs text-muted-foreground">留空关闭定时触发</p>
                </div>
              </div>
            </div>
          </div>

          {/* Prompt */}
          <div className="space-y-1.5">
            <Label htmlFor="tidy-prompt">Prompt 模板</Label>
            <Textarea
              id="tidy-prompt"
              rows={25}
              className="font-mono text-xs"
              {...register('prompt')}
            />
            {errors.prompt ? <p className="text-xs text-destructive">{errors.prompt.message}</p> : null}
          </div>

          <Button type="submit" disabled={isSubmitting}>保存</Button>
        </form>
      </CardContent>
    </Card>
  );
}
