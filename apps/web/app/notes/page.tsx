'use client';

import * as React from 'react';
import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { api, ApiError } from '@/lib/api';
import type { Note, AIOpsResponse } from '@/lib/types';
import { MarkdownView } from '@/components/markdown-view';
import { cn } from '@/lib/utils';
import { Button } from '@/components/ui/button';
import { Checkbox } from '@/components/ui/checkbox';
import { toast } from '@/components/ui/toaster';
import { AIActionDialog } from './ai-action-dialog';

interface TreeNode {
  name: string;
  path: string;
  children: Map<string, TreeNode>;
  notes: Note[];
}

function buildTree(notes: Note[]): TreeNode {
  const root: TreeNode = { name: '', path: '', children: new Map(), notes: [] };
  for (const n of notes) {
    const cat = (n.category ?? '').replace(/^\/+|\/+$/g, '');
    if (!cat) {
      root.notes.push(n);
      continue;
    }
    const segs = cat.split('/').filter(Boolean);
    let cur = root;
    for (let i = 0; i < segs.length; i++) {
      const seg = segs[i] as string;
      const childPath = segs.slice(0, i + 1).join('/');
      let child = cur.children.get(seg);
      if (!child) {
        child = { name: seg, path: childPath, children: new Map(), notes: [] };
        cur.children.set(seg, child);
      }
      cur = child;
    }
    cur.notes.push(n);
  }
  return root;
}

function TreeView({
  node,
  selected,
  onSelect,
  multiSelectMode,
  selectedPaths,
  onToggleSelected,
  onSelectFolder,
}: {
  node: TreeNode;
  selected: string | null;
  onSelect: (path: string) => void;
  multiSelectMode: boolean;
  selectedPaths: Set<string>;
  onToggleSelected: (path: string) => void;
  onSelectFolder: (folderPath: string) => void;
}): JSX.Element {
  return (
    <ul className="space-y-1 text-sm">
      {[...node.children.values()].map((child) => (
        <li key={child.path}>
          <details open className="group">
            <summary className="cursor-pointer rounded px-2 py-1 font-medium hover:bg-accent flex items-center gap-2">
              <span className="flex-1">{child.name}</span>
              {multiSelectMode ? (
                <button
                  onClick={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    onSelectFolder(`notes/${child.path}`);
                  }}
                  className="text-xs text-muted-foreground hover:text-foreground"
                  title="选中此文件夹下所有笔记"
                >
                  全选
                </button>
              ) : null}
            </summary>
            <div className="ml-3 border-l pl-2">
              <TreeView
                node={child}
                selected={selected}
                onSelect={onSelect}
                multiSelectMode={multiSelectMode}
                selectedPaths={selectedPaths}
                onToggleSelected={onToggleSelected}
                onSelectFolder={onSelectFolder}
              />
            </div>
          </details>
        </li>
      ))}
      {node.notes.map((n) => (
        <li key={n.path} className="flex items-center gap-1">
          {multiSelectMode ? (
            <Checkbox
              checked={selectedPaths.has(n.path)}
              onCheckedChange={() => onToggleSelected(n.path)}
              className="ml-2"
            />
          ) : null}
          <button
            onClick={() => onSelect(n.path)}
            className={cn(
              'flex-1 truncate rounded px-2 py-1 text-left hover:bg-accent',
              selected === n.path && 'bg-accent text-accent-foreground',
            )}
            title={n.path}
          >
            {n.title || n.path}
          </button>
        </li>
      ))}
    </ul>
  );
}

function NotesBrowser(): JSX.Element {
  const search = useSearchParams();
  const initialPath = search.get('path');
  const [notes, setNotes] = React.useState<Note[]>([]);
  const [loading, setLoading] = React.useState(true);
  const [selectedPath, setSelectedPath] = React.useState<string | null>(initialPath);
  const [selected, setSelected] = React.useState<Note | null>(null);
  const [error, setError] = React.useState<string | null>(null);

  // Multi-select state.
  const [multiSelectMode, setMultiSelectMode] = React.useState(false);
  const [selectedPaths, setSelectedPaths] = React.useState<Set<string>>(new Set());
  const [aiDialogOpen, setAiDialogOpen] = React.useState(false);
  const [aiAction, setAiAction] = React.useState<
    'deep_tidy' | 'polish_logic' | 'rewrite' | 'knowledge_check' | null
  >(null);
  const [aiPreview, setAiPreview] = React.useState<AIOpsResponse | null>(null);
  const [aiBusy, setAiBusy] = React.useState(false);

  const loadNotes = React.useCallback(async () => {
    try {
      const list = await api.list();
      setNotes(Array.isArray(list) ? list : []);
    } catch (e) {
      setError(e instanceof ApiError ? e.message : '加载失败');
    } finally {
      setLoading(false);
    }
  }, []);

  React.useEffect(() => {
    void loadNotes();
  }, [loadNotes]);

  React.useEffect(() => {
    if (!selectedPath) {
      setSelected(null);
      return;
    }
    let cancelled = false;
    (async () => {
      try {
        const n = await api.note(selectedPath);
        if (!cancelled) setSelected(n);
      } catch (e) {
        if (!cancelled) setError(e instanceof ApiError ? e.message : '加载失败');
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [selectedPath]);

  const tree = React.useMemo(() => buildTree(notes), [notes]);

  const toggleSelected = (path: string) => {
    setSelectedPaths((prev) => {
      const next = new Set(prev);
      if (next.has(path)) next.delete(path);
      else next.add(path);
      return next;
    });
  };

  const selectFolder = (folderPath: string) => {
    // Add all notes whose path starts with `folderPath/` to selection.
    const prefix = folderPath.replace(/\/+$/, '') + '/';
    setSelectedPaths((prev) => {
      const next = new Set(prev);
      for (const n of notes) {
        if (n.path.startsWith(prefix)) {
          next.add(n.path);
        }
      }
      return next;
    });
  };

  const triggerAIAction = async (
    action: 'deep_tidy' | 'polish_logic' | 'rewrite' | 'knowledge_check',
  ) => {
    if (selectedPaths.size === 0) {
      toast({ description: '请先选中至少一篇笔记', variant: 'destructive' });
      return;
    }
    setAiAction(action);
    setAiBusy(true);
    setAiDialogOpen(true);
    setAiPreview(null);
    try {
      const resp = await api.aiAction({
        action,
        paths: Array.from(selectedPaths),
        preview_only: true,
      });
      setAiPreview(resp);
    } catch (e) {
      toast({ description: e instanceof Error ? e.message : '请求失败', variant: 'destructive' });
      setAiDialogOpen(false);
    } finally {
      setAiBusy(false);
    }
  };

  const confirmApply = async () => {
    if (!aiPreview) return;
    setAiBusy(true);
    try {
      await api.applyAiAction(aiPreview);
      toast({ description: '已应用' });
      setAiDialogOpen(false);
      setSelectedPaths(new Set());
      setMultiSelectMode(false);
      await loadNotes();
    } catch (e) {
      toast({ description: e instanceof Error ? e.message : '应用失败', variant: 'destructive' });
    } finally {
      setAiBusy(false);
    }
  };

  return (
    <div className="grid h-[calc(100vh-3rem)] grid-cols-12">
      <aside className="col-span-12 md:col-span-3 overflow-y-auto border-r p-3">
        <div className="mb-2 flex items-center justify-between">
          <h2 className="text-sm font-semibold">笔记</h2>
          <div className="flex items-center gap-2">
            <button
              onClick={() => {
                setMultiSelectMode((v) => !v);
                if (multiSelectMode) setSelectedPaths(new Set());
              }}
              className={cn(
                'text-xs',
                multiSelectMode ? 'text-foreground font-medium' : 'text-muted-foreground hover:text-foreground',
              )}
            >
              {multiSelectMode ? '退出多选' : '多选'}
            </button>
            <Link href="/capture" className="text-xs text-muted-foreground hover:text-foreground">
              + 快捕
            </Link>
          </div>
        </div>

        {multiSelectMode && selectedPaths.size > 0 ? (
          <div className="mb-2 space-y-1 rounded border bg-muted/30 p-2 text-xs">
            <div className="text-muted-foreground">已选 {selectedPaths.size} 篇</div>
            <div className="grid grid-cols-2 gap-1">
              <Button size="sm" variant="outline" onClick={() => triggerAIAction('deep_tidy')}>
                深度整理
              </Button>
              <Button size="sm" variant="outline" onClick={() => triggerAIAction('polish_logic')}>
                润色
              </Button>
              <Button size="sm" variant="outline" onClick={() => triggerAIAction('rewrite')}>
                重写
              </Button>
              <Button size="sm" variant="outline" onClick={() => triggerAIAction('knowledge_check')}>
                审查
              </Button>
            </div>
            <button
              onClick={() => setSelectedPaths(new Set())}
              className="w-full text-muted-foreground hover:text-foreground"
            >
              清空选择
            </button>
          </div>
        ) : null}

        {loading ? (
          <p className="text-sm text-muted-foreground">加载中…</p>
        ) : error ? (
          <p className="text-sm text-destructive">{error}</p>
        ) : notes.length === 0 ? (
          <p className="text-sm text-muted-foreground">还没有整理过的笔记。</p>
        ) : (
          <TreeView
            node={tree}
            selected={selectedPath}
            onSelect={setSelectedPath}
            multiSelectMode={multiSelectMode}
            selectedPaths={selectedPaths}
            onToggleSelected={toggleSelected}
            onSelectFolder={selectFolder}
          />
        )}
      </aside>
      <section className="col-span-12 md:col-span-9 overflow-y-auto p-6">
        {selected ? (
          <article className="mx-auto max-w-3xl">
            <header className="mb-4">
              <h1 className="text-2xl font-semibold">{selected.title || selected.path}</h1>
              <p className="text-xs text-muted-foreground">{selected.path}</p>
              {selected.tags && selected.tags.length > 0 ? (
                <p className="mt-2 flex flex-wrap gap-1 text-xs">
                  {selected.tags.map((t) => (
                    <span key={t} className="rounded bg-muted px-2 py-0.5">
                      #{t}
                    </span>
                  ))}
                </p>
              ) : null}
              {selected.frontmatter && Object.keys(selected.frontmatter).length > 0 ? (
                <details className="mt-2 text-xs text-muted-foreground">
                  <summary className="cursor-pointer">frontmatter</summary>
                  <pre className="mt-1 overflow-x-auto rounded bg-muted p-2">
                    {JSON.stringify(selected.frontmatter, null, 2)}
                  </pre>
                </details>
              ) : null}
            </header>
            <MarkdownView content={selected.body ?? ''} />
            <p className="mt-8 text-xs text-muted-foreground">编辑请在 Obsidian 中进行。</p>
          </article>
        ) : (
          <div className="grid h-full place-items-center text-sm text-muted-foreground">
            选择左侧笔记查看
          </div>
        )}
      </section>

      <AIActionDialog
        open={aiDialogOpen}
        onClose={() => setAiDialogOpen(false)}
        action={aiAction}
        preview={aiPreview}
        busy={aiBusy}
        onConfirm={confirmApply}
      />
    </div>
  );
}

export default function NotesPage(): JSX.Element {
  return (
    <React.Suspense fallback={<div className="p-4 text-sm text-muted-foreground">加载中…</div>}>
      <NotesBrowser />
    </React.Suspense>
  );
}
