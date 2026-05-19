// Offline capture queue, persisted in IndexedDB via Dexie.
// On `/capture`, writes go straight to API when online; when offline or the
// request fails, they're enqueued here and flushed on the next online event.

import Dexie, { type Table } from 'dexie';

export interface QueuedCapture {
  id?: number;
  content: string;
  ts: string;
  status: 'pending' | 'sending' | 'sent' | 'failed';
  attempts: number;
  lastError?: string;
}

class CaptureDB extends Dexie {
  captures!: Table<QueuedCapture, number>;

  constructor() {
    super('knowlib');
    this.version(1).stores({
      captures: '++id, status, ts',
    });
  }
}

let _db: CaptureDB | null = null;

export function db(): CaptureDB {
  if (typeof window === 'undefined') {
    throw new Error('IndexedDB is browser-only');
  }
  if (!_db) _db = new CaptureDB();
  return _db;
}

export async function enqueueCapture(content: string): Promise<number> {
  return db().captures.add({
    content,
    ts: new Date().toISOString(),
    status: 'pending',
    attempts: 0,
  });
}

export async function pendingCount(): Promise<number> {
  return db().captures.where('status').anyOf('pending', 'failed').count();
}

export async function listRecent(limit = 5): Promise<QueuedCapture[]> {
  return db().captures.orderBy('ts').reverse().limit(limit).toArray();
}

export async function flushQueue(
  send: (content: string) => Promise<void>,
): Promise<{ sent: number; failed: number }> {
  const pending = await db().captures.where('status').anyOf('pending', 'failed').toArray();
  let sent = 0;
  let failed = 0;
  for (const item of pending) {
    if (item.id == null) continue;
    await db().captures.update(item.id, { status: 'sending' });
    try {
      await send(item.content);
      await db().captures.update(item.id, { status: 'sent' });
      sent += 1;
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);
      await db().captures.update(item.id, {
        status: 'failed',
        attempts: (item.attempts ?? 0) + 1,
        lastError: msg,
      });
      failed += 1;
    }
  }
  return { sent, failed };
}
