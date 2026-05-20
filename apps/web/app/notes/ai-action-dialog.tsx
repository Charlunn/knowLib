'use client';

import * as React from 'react';
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog';
import { Button } from '@/components/ui/button';
import { cn } from '@/lib/utils';
import type { AIOpsResponse, AIOpsOperation } from '@/lib/types';

const ACTION_LABELS: Record<string, string> = {
  deep_tidy: '深度整理',
  polish_logic: '润色',
  rewrite: '重写',
  knowledge_check: '知识审查',
};

const OP_TYPE_LABELS: Record<string, { label: string; color: string }> = {
  create: { label: '新建', color: 'text-emerald-600' },
  modify: { label: '修改', color: 'text-blue-600' },
  rename: { label: '移动', color: 'text-amber-600' },
  delete: { label: '删除', color: 'text-destructive' },
};

export function AIActionDialog({
  open,
  onClose,
  action,
  preview,
  busy,
  onConfirm,
}: {
  open: boolean;
  onClose: () => void;
  action: string | null;
  preview: AIOpsResponse | null;
  busy: boolean;
  onConfirm: () => void;
}): JSX.Element {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-4xl max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>
            {action ? ACTION_LABELS[action] ?? action : 'AI 操作'} — 预览
          </DialogTitle>
          <DialogDescription>
            {busy && !preview
              ? 'AI 正在分析,可能需要 30 秒到几分钟...'
              : preview
                ? preview.summary || `共 ${preview.operations.length} 个操作`
                : '加载中...'}
          </DialogDescription>
        </DialogHeader>

        {preview ? (
          <div className="space-y-3">
            {preview.operations.length === 0 ? (
              <p className="text-sm text-muted-foreground">AI 没有提议任何变更。</p>
            ) : (
              preview.operations.map((op, i) => <OperationCard key={i} op={op} />)
            )}

            <div className="flex justify-end gap-2 pt-4 border-t">
              <Button variant="outline" onClick={onClose} disabled={busy}>
                取消
              </Button>
              <Button onClick={onConfirm} disabled={busy || preview.operations.length === 0}>
                {busy ? '应用中…' : `确认应用 ${preview.operations.length} 个变更`}
              </Button>
            </div>
          </div>
        ) : null}
      </DialogContent>
    </Dialog>
  );
}

function OperationCard({ op }: { op: AIOpsOperation }): JSX.Element {
  const typeInfo = OP_TYPE_LABELS[op.type] ?? { label: op.type, color: 'text-foreground' };

  const pathLabel =
    op.type === 'rename' ? (
      <>
        <span className="line-through text-muted-foreground">{op.from}</span>
        <span className="mx-2">→</span>
        <span>{op.to}</span>
      </>
    ) : (
      op.path ?? ''
    );

  return (
    <div className="rounded border p-3 text-sm space-y-2">
      <div className="flex items-baseline gap-2">
        <span className={cn('font-medium text-xs uppercase', typeInfo.color)}>
          {typeInfo.label}
        </span>
        <span className="font-mono text-xs">{pathLabel}</span>
      </div>

      {op.summary ? <p className="text-xs text-muted-foreground">{op.summary}</p> : null}
      {op.reason ? (
        <p className="text-xs text-muted-foreground">原因: {op.reason}</p>
      ) : null}

      {op.issues && op.issues.length > 0 ? (
        <details className="text-xs">
          <summary className="cursor-pointer text-muted-foreground">
            发现 {op.issues.length} 处问题
          </summary>
          <ul className="mt-2 space-y-2">
            {op.issues.map((issue, i) => (
              <li key={i} className="rounded border-l-2 pl-2 border-amber-400">
                <div className="font-medium">{issue.kind}</div>
                {issue.original ? (
                  <div className="text-muted-foreground">原: {issue.original}</div>
                ) : null}
                {issue.fixed ? <div className="text-emerald-600">改: {issue.fixed}</div> : null}
                {issue.note ? <div className="text-amber-600">注: {issue.note}</div> : null}
                {issue.marked_as ? <div className="text-blue-600">→ {issue.marked_as}</div> : null}
                {issue.explanation ? (
                  <div className="text-muted-foreground italic">{issue.explanation}</div>
                ) : null}
              </li>
            ))}
          </ul>
        </details>
      ) : null}

      {op.type !== 'delete' && op.content ? (
        <details className="text-xs">
          <summary className="cursor-pointer text-muted-foreground">查看新内容</summary>
          <pre className="mt-2 overflow-x-auto rounded bg-muted/50 p-2 text-xs whitespace-pre-wrap">
            {op.content}
          </pre>
        </details>
      ) : null}

      {op.before && (op.type === 'modify' || op.type === 'rename' || op.type === 'delete') ? (
        <details className="text-xs">
          <summary className="cursor-pointer text-muted-foreground">查看原内容</summary>
          <pre className="mt-2 overflow-x-auto rounded bg-muted/50 p-2 text-xs whitespace-pre-wrap">
            {op.before}
          </pre>
        </details>
      ) : null}
    </div>
  );
}
