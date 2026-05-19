'use client';

import * as React from 'react';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { LlmSection } from './sections/llm';
import { EmbedSection } from './sections/embed';
import { TidySection } from './sections/tidy';
import { TokensSection } from './sections/tokens';
import { SkillSection } from './sections/skill';
import { TotpSection } from './sections/totp';
import { api } from '@/lib/api';
import type { Settings } from '@/lib/types';

export function SettingsTabs(): JSX.Element {
  const [settings, setSettings] = React.useState<Settings | null>(null);
  const [error, setError] = React.useState<string | null>(null);

  const reload = React.useCallback(async () => {
    try {
      setSettings(await api.getSettings());
    } catch (e) {
      setError(e instanceof Error ? e.message : '加载失败');
    }
  }, []);

  React.useEffect(() => {
    void reload();
  }, [reload]);

  if (error) return <p className="text-destructive">{error}</p>;
  if (!settings) return <p className="text-sm text-muted-foreground">加载中…</p>;

  return (
    <Tabs defaultValue="llm">
      <TabsList className="flex flex-wrap">
        <TabsTrigger value="llm">LLM</TabsTrigger>
        <TabsTrigger value="embed">Embedding</TabsTrigger>
        <TabsTrigger value="tidy">整理</TabsTrigger>
        <TabsTrigger value="tokens">API Tokens</TabsTrigger>
        <TabsTrigger value="skill">Skill 包</TabsTrigger>
        <TabsTrigger value="totp">TOTP</TabsTrigger>
      </TabsList>
      <TabsContent value="llm">
        <LlmSection settings={settings} onSaved={reload} />
      </TabsContent>
      <TabsContent value="embed">
        <EmbedSection settings={settings} onSaved={reload} />
      </TabsContent>
      <TabsContent value="tidy">
        <TidySection settings={settings} onSaved={reload} />
      </TabsContent>
      <TabsContent value="tokens">
        <TokensSection />
      </TabsContent>
      <TabsContent value="skill">
        <SkillSection />
      </TabsContent>
      <TabsContent value="totp">
        <TotpSection />
      </TabsContent>
    </Tabs>
  );
}
