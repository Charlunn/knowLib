// Bridge to @vrtmrz/livesync-commonlib.
//
// ⚠ STATUS: scaffold only. The two functions below are the integration seam
// between CouchDB documents (the LiveSync wire format) and plain markdown
// files on disk. They are intentionally left as `NotImplementedError` so the
// rest of the service can be reviewed and shipped with an honest boundary,
// rather than guessing the format and risking silent data corruption against
// the real Obsidian plugin.
//
// To finish:
//   1. `pnpm add @vrtmrz/livesync-commonlib`
//   2. Read its README for the public assemble/split functions
//      (typically something like buildContent / splitContent + EncryptionHelper).
//   3. Replace the bodies below.
//
// Why this isn't done in this scaffold: the build environment that produced
// this code did not have npm registry access for that package, and the format
// is not documented in a way that's safe to reverse-engineer blind. The
// README explains the implications and the workaround for Phase 1.

import type { DocLike } from './couch';

export class NotImplementedError extends Error {}

export interface VaultFile {
  /** Forward-slashed, vault-relative path: "inbox/x.md", "notes/学习/y.md". */
  path: string;
  /** Decrypted file contents. */
  body: Buffer;
}

export class LiveSyncCodec {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  constructor(private readonly e2ePassphrase: string) {}

  /**
   * Decode a CouchDB document (as produced by Self-hosted LiveSync) into a
   * vault file. Returns `null` if the doc is internal (e.g. _design docs,
   * LiveSync's own bookkeeping documents) and shouldn't be mirrored.
   */
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  assembleFromCouchDoc(_doc: DocLike): VaultFile | null {
    throw new NotImplementedError(
      'LiveSync doc assembly is not yet implemented in this build. ' +
        'Install @vrtmrz/livesync-commonlib and replace this stub.',
    );
  }

  /**
   * Encode a vault file as a CouchDB document (or a set of documents, if the
   * file is large enough that LiveSync chunks it). Caller is responsible for
   * applying these in CouchDB in the right order.
   */
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  splitForCouchDoc(_file: VaultFile): DocLike[] {
    throw new NotImplementedError(
      'LiveSync doc splitting is not yet implemented in this build. ' +
        'Install @vrtmrz/livesync-commonlib and replace this stub.',
    );
  }

  /**
   * Returns true iff the given doc id is one LiveSync uses for its own
   * bookkeeping (e.g. config docs, chunk dedup pointers, history shards).
   * The LiveSync source has the canonical list; until we can import it,
   * skip the obvious ones to avoid mirroring noise to disk.
   */
  isInternalId(id: string): boolean {
    if (id.startsWith('_')) return true; // _design, _local, _users etc.
    if (id.startsWith('h:')) return true; // history shard hint
    if (id.startsWith('chunk-')) return true; // chunk dedup hint
    return false;
  }
}
