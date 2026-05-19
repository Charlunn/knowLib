// Per-path bookkeeping in a leveldb. Two purposes:
//   1. Resume from the right CouchDB _changes seq after a restart.
//   2. Detect "echo" events: when we write a file ourselves (because CouchDB
//      told us to), the chokidar watcher fires; we'd then push it back up to
//      CouchDB, get another _changes notification, and loop forever. Each
//      mirrored write records the path → (rev, mtimeMs); the watcher checks
//      this map and skips events whose mtime matches the recorded one.

import { Level } from 'level';
import { join } from 'path';

interface PathRecord {
  rev: string;
  mtimeMs: number;
}

export class State {
  private readonly db: Level<string, string>;

  constructor(stateDir: string) {
    this.db = new Level<string, string>(join(stateDir, 'mirror-state'), { valueEncoding: 'utf8' });
  }

  async open(): Promise<void> {
    await this.db.open();
  }

  async close(): Promise<void> {
    await this.db.close();
  }

  async getCouchSeq(): Promise<string | null> {
    try {
      return await this.db.get('couch:seq');
    } catch (err: unknown) {
      if ((err as { code?: string }).code === 'LEVEL_NOT_FOUND') return null;
      throw err;
    }
  }

  async setCouchSeq(seq: string): Promise<void> {
    await this.db.put('couch:seq', seq);
  }

  private pathKey(p: string): string {
    return 'path:' + p;
  }

  async recordWrite(path: string, rev: string, mtimeMs: number): Promise<void> {
    await this.db.put(this.pathKey(path), JSON.stringify({ rev, mtimeMs } satisfies PathRecord));
  }

  async getRecord(path: string): Promise<PathRecord | null> {
    try {
      const raw = await this.db.get(this.pathKey(path));
      return JSON.parse(raw) as PathRecord;
    } catch (err: unknown) {
      if ((err as { code?: string }).code === 'LEVEL_NOT_FOUND') return null;
      throw err;
    }
  }

  async forgetPath(path: string): Promise<void> {
    await this.db.del(this.pathKey(path)).catch(() => {});
  }
}
