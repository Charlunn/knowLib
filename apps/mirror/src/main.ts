// vault-mirror entrypoint.
//
// Loop:
//   - Watch CouchDB _changes; for each doc, decode via LiveSyncCodec and
//     either write the file under VAULT_PATH or delete it.
//   - Watch the filesystem under VAULT_PATH; for each non-echo change,
//     encode via LiveSyncCodec and push to CouchDB.
//
// Echoes between the two halves are suppressed by the per-path record in
// state.ts: every mirror-driven write notes (path, rev, mtimeMs); when the
// watcher sees an event whose mtimeMs matches the record, it skips.
//
// As of this commit the LiveSyncCodec methods are NotImplementedError stubs.
// The watcher will log a clear message and idle in that case so users can
// validate the surrounding plumbing (compose volumes, env vars, CouchDB
// connectivity) before swapping the codec in.

import chokidar from 'chokidar';
import { Couch } from './couch';
import { loadConfig } from './config';
import { isUnderHidden, SafeFS } from './fs';
import { LiveSyncCodec, NotImplementedError } from './livesync';
import { State } from './state';

async function main(): Promise<void> {
  const cfg = loadConfig();
  const fs = new SafeFS(cfg.vaultPath);
  const state = new State(cfg.stateDir);
  await state.open();

  const couch = new Couch(cfg.couchUrl, cfg.couchDb);
  const codec = new LiveSyncCodec(cfg.e2ePassphrase);

  let codecWarned = false;
  function warnCodecMissing(where: string, err: unknown): void {
    if (codecWarned) return;
    codecWarned = true;
    console.warn(
      '[mirror] LiveSyncCodec is a stub in this build (%s). Install ' +
        '@vrtmrz/livesync-commonlib and finish src/livesync.ts. The ' +
        'mirror is otherwise running and will start syncing as soon as ' +
        'the codec is in place. (root cause: %s)',
      where,
      (err as Error).message,
    );
  }

  // === CouchDB → filesystem =================================================

  const since = (await state.getCouchSeq()) ?? 'now';
  console.log('[mirror] starting CouchDB _changes feed since=%s', since);
  couch.watchChanges({
    since,
    onChange: async (c) => {
      if (!c.doc) return;
      if (codec.isInternalId(c.id)) return;

      try {
        if (c.deleted) {
          // Translate the LiveSync delete into a vault-relative path. This
          // also goes through the codec because LiveSync's id/path mapping
          // can be non-trivial (chunked docs, etc.).
          const file = codec.assembleFromCouchDoc(c.doc);
          if (!file) return;
          await fs.remove(file.path);
          await state.forgetPath(file.path);
          console.log('[mirror] couch->fs DELETE %s', file.path);
        } else {
          const file = codec.assembleFromCouchDoc(c.doc);
          if (!file) return;
          if (isUnderHidden(file.path, cfg.knowlibDir)) return;
          await fs.writeAtomic(file.path, file.body);
          const m = await fs.statMtime(file.path);
          if (m !== null) {
            await state.recordWrite(file.path, c.doc._rev ?? '', m);
          }
          console.log('[mirror] couch->fs WRITE %s (rev=%s)', file.path, c.doc._rev);
        }
        if (c.seq) await state.setCouchSeq(String(c.seq));
      } catch (err) {
        if (err instanceof NotImplementedError) {
          warnCodecMissing('changes feed', err);
          return;
        }
        console.error('[mirror] couch->fs error on doc %s: %s', c.id, (err as Error).message);
      }
    },
    onError: (err) => {
      console.error('[mirror] _changes feed error: %s', err.message);
    },
  });

  // === filesystem → CouchDB ================================================

  const watcher = chokidar.watch(cfg.vaultPath, {
    ignoreInitial: true,
    ignored: (p: string): boolean => {
      // Skip hidden dirs and our own state. We DON'T re-emit on .knowlib —
      // it's local-only.
      const rel = p.replace(/\\/g, '/').replace(cfg.vaultPath.replace(/\\/g, '/') + '/', '');
      return isUnderHidden(rel, cfg.knowlibDir);
    },
  });

  const debounced = new Map<string, NodeJS.Timeout>();
  const debounceMs = 500;

  async function pushToCouch(absPath: string, kind: 'change' | 'unlink'): Promise<void> {
    const rel = SafeFS.normaliseVaultPath(
      absPath.replace(cfg.vaultPath, '').replace(/^[\/\\]/, ''),
    );
    if (rel === '' || isUnderHidden(rel, cfg.knowlibDir)) return;

    // Echo suppression: if the file's mtime matches what we last wrote, this
    // event is the watcher seeing our own write — skip.
    const rec = await state.getRecord(rel);
    const mtime = await fs.statMtime(rel);
    if (kind !== 'unlink' && rec && mtime !== null && Math.abs(rec.mtimeMs - mtime) < 1) {
      return;
    }

    try {
      if (kind === 'unlink') {
        // Delete in CouchDB. LiveSync uses content-derived ids, so the codec
        // could compute the id from the path — but with the stubbed codec we
        // can't. Log and bail; the real fix is the codec.
        // eslint-disable-next-line @typescript-eslint/no-unused-vars
        const file = { path: rel, body: Buffer.alloc(0) };
        const docs = codec.splitForCouchDoc(file);
        for (const d of docs) {
          if (d._rev) await couch.remove(d._id, d._rev);
        }
        await state.forgetPath(rel);
        console.log('[mirror] fs->couch DELETE %s', rel);
      } else {
        const body = await fs.read(rel);
        const docs = codec.splitForCouchDoc({ path: rel, body });
        for (const d of docs) {
          await couch.put(d);
        }
        if (mtime !== null) await state.recordWrite(rel, '', mtime);
        console.log('[mirror] fs->couch WRITE %s', rel);
      }
    } catch (err) {
      if (err instanceof NotImplementedError) {
        warnCodecMissing('watcher', err);
        return;
      }
      console.error('[mirror] fs->couch error on %s: %s', rel, (err as Error).message);
    }
  }

  function schedule(absPath: string, kind: 'change' | 'unlink'): void {
    const prev = debounced.get(absPath);
    if (prev) clearTimeout(prev);
    debounced.set(
      absPath,
      setTimeout(() => {
        debounced.delete(absPath);
        void pushToCouch(absPath, kind);
      }, debounceMs),
    );
  }

  watcher.on('add', (p) => schedule(p, 'change'));
  watcher.on('change', (p) => schedule(p, 'change'));
  watcher.on('unlink', (p) => schedule(p, 'unlink'));
  watcher.on('error', (err) => console.error('[mirror] watcher error: %s', (err as Error).message));

  console.log('[mirror] watching %s', cfg.vaultPath);

  // === lifecycle ============================================================

  const shutdown = async (sig: string): Promise<void> => {
    console.log('[mirror] %s received; shutting down', sig);
    await watcher.close().catch(() => {});
    await state.close().catch(() => {});
    process.exit(0);
  };
  process.on('SIGINT', () => void shutdown('SIGINT'));
  process.on('SIGTERM', () => void shutdown('SIGTERM'));
}

main().catch((err: unknown) => {
  console.error('[mirror] fatal: %s', (err as Error).stack ?? err);
  process.exit(1);
});
