'use client';

import * as React from 'react';
import { useForm } from 'react-hook-form';
import { zodResolver } from '@hookform/resolvers/zod';
import { z } from 'zod';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card';
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group';
import { api } from '@/lib/api';
import { toast } from '@/components/ui/toaster';
import type { Settings, TidyMode } from '@/lib/types';

const Schema = z.object({
  top_k: z.coerce.number().int().min(1).max(64),
  max_tokens: z.coerce.number().int().min(256).max(32768),
  mode: z.enum(['manual', 'scheduled', 'threshold']),
  prompt: z.string().min(1),
});
type FormVals = z.infer<typeof Schema>;

export function TidySection({ settings, onSaved }: { settings: Settings; onSaved: () => Promise<void> }): JSX.Element {
  const initialMode: TidyMode = settings.tidy?.mode ?? 'manual';
  const [mode, setMode] = React.useState<TidyMode>(initialMode);
  const { register, handleSubmit, setValue, formState: { errors, isSubmitting } } = useForm<FormVals>({
    resolver: zodResolver(Schema),
    defaultValues: {
      top_k: settings.tidy?.top_k ?? 8,
      max_tokens: settings.tidy?.max_tokens ?? 4096,
      mode: initialMode,
      prompt: settings.tidy?.prompt ?? '',
    },
  });

  const onSubmit = handleSubmit(async (vals) => {
    try {
      await api.updateSettings({ tidy: vals });
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
        <form className="space-y-4" onSubmit={onSubmit}>
          <div className="grid grid-cols-2 gap-4">
            <div className="space-y-1.5">
              <Label htmlFor="tidy-topk">Top-K</Label>
              <Input id="tidy-topk" type="number" {...register('top_k')} />
              {errors.top_k ? <p className="text-xs text-destructive">{errors.top_k.message}</p> : null}
            </div>
            <div className="space-y-1.5">
              <Label htmlFor="tidy-mt">Max Tokens</Label>
              <Input id="tidy-mt" type="number" {...register('max_tokens')} />
              {errors.max_tokens ? <p className="text-xs text-destructive">{errors.max_tokens.message}</p> : null}
            </div>
          </div>

          <div className="space-y-2">
            <Label>触发模式</Label>
            <RadioGroup
              value={mode}
              onValueChange={(v) => {
                const next = v as TidyMode;
                setMode(next);
                setValue('mode', next);
              }}
              className="space-y-1"
            >
              <div className="flex items-center gap-2">
                <RadioGroupItem id="m-manual" value="manual" />
                <Label htmlFor="m-manual">仅手动</Label>
              </div>
              <div className="flex items-center gap-2 opacity-60">
                <RadioGroupItem id="m-sched" value="scheduled" disabled />
                <Label htmlFor="m-sched">定时(Phase 2)</Label>
              </div>
              <div className="flex items-center gap-2 opacity-60">
                <RadioGroupItem id="m-thresh" value="threshold" disabled />
                <Label htmlFor="m-thresh">阈值(Phase 2)</Label>
              </div>
            </RadioGroup>
          </div>

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
