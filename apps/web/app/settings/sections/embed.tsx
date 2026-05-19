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
  base_url: z.string().url(),
  model: z.string().min(1),
});
type FormVals = z.infer<typeof Schema>;

export function EmbedSection({ settings, onSaved }: { settings: Settings; onSaved: () => Promise<void> }): JSX.Element {
  const { register, handleSubmit, formState: { errors, isSubmitting } } = useForm<FormVals>({
    resolver: zodResolver(Schema),
    defaultValues: {
      base_url: settings.embed?.base_url ?? '',
      model: settings.embed?.model ?? '',
    },
  });

  const onSubmit = handleSubmit(async (vals) => {
    try {
      await api.updateSettings({ embed: vals });
      toast({ description: '已保存' });
      await onSaved();
    } catch (e) {
      toast({ description: e instanceof Error ? e.message : '保存失败', variant: 'destructive' });
    }
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Embedding</CardTitle>
      </CardHeader>
      <CardContent>
        <form className="space-y-4" onSubmit={onSubmit}>
          <div className="space-y-1.5">
            <Label htmlFor="embed-base">Base URL</Label>
            <Input id="embed-base" {...register('base_url')} placeholder="http://embedder:8000/v1" />
            {errors.base_url ? <p className="text-xs text-destructive">{errors.base_url.message}</p> : null}
          </div>
          <div className="space-y-1.5">
            <Label htmlFor="embed-model">Model</Label>
            <Input id="embed-model" {...register('model')} placeholder="bge-m3" />
            {errors.model ? <p className="text-xs text-destructive">{errors.model.message}</p> : null}
          </div>
          <Button type="submit" disabled={isSubmitting}>保存</Button>
        </form>
      </CardContent>
    </Card>
  );
}
