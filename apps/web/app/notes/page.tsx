'use client';

import * as React from 'react';
import Link from 'next/link';
import { useSearchParams } from 'next/navigation';
import { api, ApiError } from '@/lib/api';
import type { Note } from '@/lib/types';
import { MarkdownView } from '@/components/markdown-view';
import { cn } from '@/lib/utils';

interface TreeNode {
  name: string;
  path: string; // category path, e.g. "学习/高数"
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
}: {
  node: TreeNode;
  selected: string | null;
  onSelect: (path: string) => void;
}): JSX.Element {
  return (
    <ul className="space-y-1 text-sm">
      {[...node.children.values()].map((child) => (
        <li key={child.path}>
          <details open className="group">
            <summary className="cursor-pointer rounded px-2 py-1 font-medium hover:bg-accent">
              {child.name}
            </summary>
            <div className="ml-3 border-l pl-2">
              <TreeView node={child} selected={selected} onSelect={onSelect} />
            </div>
          </details>
        </li>
      ))}
      {node.notes.map((n) => (
        <li key={n.path}>
          <button
            onClick={() => onSelect(n.path)}
            className={cn(
              'w-full truncate rounded px-2 py-1 text-left hover:bg-accent',
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

  React.useEffect(() => {
    let cancelled = false;
    (async () => {
      try {
        const list = await api.list();
        if (!cancelled) setNotes(list);
      } catch (e) {
        if (!cancelled) setError(e instanceof ApiError ? e.message : '加载失败');
      } finally {
        if (!cancelled) setLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

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

  return (
    <div className="grid h-[calc(100vh-3rem)] grid-cols-12">
      <aside className="col-span-12 md:col-span-3 overflow-y-auto border-r p-3">
        <div className="mb-2 flex items-center justify-between">
          <h2 className="text-sm font-semibold">笔记</h2>
          <Link href="/capture" className="text-xs text-muted-foreground hover:text-foreground">
            + 快捕
          </Link>
        </div>
        {loading ? (
          <p className="text-sm text-muted-foreground">加载中…</p>
        ) : error ? (
          <p className="text-sm text-destructive">{error}</p>
        ) : notes.length === 0 ? (
          <p className="text-sm text-muted-foreground">还没有整理过的笔记。</p>
        ) : (
          <TreeView node={tree} selected={selectedPath} onSelect={setSelectedPath} />
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
    </div>
  );
}

export default function NotesPage(): JSX.Element {
  return (
    // Suspense boundary required because NotesBrowser calls useSearchParams()
    <React.Suspense fallback={<div className="p-4 text-sm text-muted-foreground">加载中…</div>}>
      <NotesBrowser />
    </React.Suspense>
  );
}
