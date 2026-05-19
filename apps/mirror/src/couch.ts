// CouchDB client wrapper. Uses `nano` for the basic surface area we need:
// - Polled `_changes` (since=last_seq, with timeout for long-poll)
// - GET document by id (with attachments)
// - PUT document
// - DELETE document
//
// LiveSync's per-document shape is opaque here — we move whole documents
// around. The shape decoding lives in livesync.ts.

import nano from 'nano';

export interface DocLike {
  _id: string;
  _rev?: string;
  _deleted?: boolean;
  [key: string]: unknown;
}

export interface ChangeItem {
  id: string;
  seq: string | number;
  deleted: boolean;
  doc: DocLike | null;
}

export class Couch {
  private readonly server: nano.ServerScope;
  readonly db: nano.DocumentScope<DocLike>;
  private stopped = false;

  constructor(url: string, dbName: string) {
    this.server = nano(url);
    this.db = this.server.db.use<DocLike>(dbName);
  }

  async get(id: string): Promise<DocLike | null> {
    try {
      const doc = await this.db.get(id, {
        attachments: true,
      } as unknown as Record<string, never>);
      return doc as unknown as DocLike;
    } catch (err: unknown) {
      const e = err as { statusCode?: number };
      if (e.statusCode === 404) return null;
      throw err;
    }
  }

  async put(doc: DocLike): Promise<{ id: string; rev: string }> {
    // nano types insist on either DocLike & MaybeDocument or ViewDocument.
    // Our DocLike _id is always present, so the cast is sound at runtime.
    const res = await this.db.insert(doc as unknown as DocLike & nano.MaybeDocument);
    return { id: res.id, rev: res.rev };
  }

  async remove(id: string, rev: string): Promise<void> {
    await this.db.destroy(id, rev);
  }

  /**
   * Long-poll the `_changes` feed in a loop until stop() is called. Each
   * change is delivered to onChange. The driver records `last_seq` on every
   * batch so the caller can persist it for crash-recovery.
   *
   * We use long-polling rather than the continuous feed because nano's typed
   * surface for the latter is awkward and the polling cost (1 idle request
   * every couch_timeout seconds) is negligible for a single-user vault.
   */
  watchChanges(opts: {
    since: string | number;
    pollTimeoutMs?: number;
    onChange: (c: ChangeItem) => Promise<void> | void;
    onError: (err: Error) => void;
  }): { stop: () => void } {
    const timeout = opts.pollTimeoutMs ?? 60_000;
    let since = opts.since;
    this.stopped = false;

    const tick = async (): Promise<void> => {
      while (!this.stopped) {
        try {
          // nano's DatabaseChangesParams type is broad; cast for the longpoll-specific bits.
          const params = {
            since,
            include_docs: true,
            attachments: true,
            feed: 'longpoll',
            timeout,
          } as unknown as nano.DatabaseChangesParams;
          const res = await this.db.changes(params);
          for (const r of res.results ?? []) {
            // res entries are typed as DatabaseChangesResultItem; the doc is
            // present because we passed include_docs=true.
            const item: ChangeItem = {
              id: r.id,
              seq: (r as unknown as { seq: string | number }).seq,
              deleted: Boolean((r as unknown as { deleted?: boolean }).deleted),
              doc: ((r as unknown as { doc?: DocLike }).doc ?? null) as DocLike | null,
            };
            try {
              await opts.onChange(item);
            } catch (err) {
              opts.onError(err as Error);
            }
          }
          since = (res as unknown as { last_seq: string | number }).last_seq ?? since;
        } catch (err) {
          opts.onError(err as Error);
          // Back off briefly so a hard error doesn't spin.
          await new Promise<void>((resolve) => setTimeout(resolve, 5_000));
        }
      }
    };

    void tick();

    return {
      stop: () => {
        this.stopped = true;
      },
    };
  }

  async info(): Promise<{ updateSeq: string }> {
    const i = await this.db.info();
    return { updateSeq: String(i.update_seq) };
  }
}
