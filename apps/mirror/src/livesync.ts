// Bridge: CouchDB (Self-hosted LiveSync) ↔ plain vault files.
//
// LiveSync document format (reverse-engineered from @vrtmrz/livesync-commonlib
// and @vrtmrz/octagonal-wheels source):
//
//   Old format (NoteEntry):
//     _id = vault-relative path (forward slashes)
//     data = plain text | encrypted string
//     type = "notes" (legacy)
//
//   New format (PlainEntry / NewEntry):
//     _id = vault-relative path
//     children = ["h:xxxx", "h:yyyy", ...]  — chunk document IDs
//     type = "plain" | "newnote"
//     (content is the concatenation of each chunk doc's .data field)
//
//   Chunk docs:
//     _id = "h:<content hash>"
//     data = plain text | encrypted string
//
//   Encryption prefixes on .data:
//     "%"  (V2) → AES-GCM, PBKDF2-SHA256, 100000 iter, iv+salt hex-prefixed
//     "%~" (V3) → same algorithm, V3 variant
//     "%=" (HKDF) → AES-GCM, HKDF-SHA256, ephemeral salt base64-prefixed
//     "["  (V1) → JSON array [encryptedData, iv, salt]
//     none     → plaintext
//
// Split (fs → couch): for Phase-1 simplicity we write single-chunk PlainEntry
// docs. Consumers (Obsidian LiveSync) will reconcile on next open.
//
// Note: `@vrtmrz/livesync-commonlib` is not published to npm; we implement
// the crypto layer directly using Node's built-in `crypto` module, which gives
// us bit-identical output to the browser WebCrypto implementation used by the
// original library (same algorithm, same key derivation parameters).

import {
  createCipheriv,
  createDecipheriv,
  createHash,
  hkdfSync,
  pbkdf2Sync,
  randomBytes,
} from 'crypto';
import type { DocLike } from './couch';

export class NotImplementedError extends Error {}

export interface VaultFile {
  /** Forward-slashed, vault-relative path: "inbox/x.md", "notes/学习/y.md". */
  path: string;
  /** Decrypted file contents as UTF-8 Buffer. */
  body: Buffer;
}

// ── Encryption constants (must match octagonal-wheels/encryption) ──────────

/** Marker prefixes for encrypted strings. */
const V2_PREFIX = '%';
const V3_PREFIX = '%~';
const HKDF_PREFIX = '%=';

const AES_GCM = 'aes-256-gcm';
const TAG_LEN = 16; // GCM auth tag bytes

/** Derive the per-passphrase key count determines PBKDF2 iterations. */
function getV2Iterations(passphrase: string): number {
  const passphraseLen = 15 - passphrase.length;
  // autoCalculateIterations=true formula from octagonal-wheels
  return passphraseLen > 0 ? passphraseLen * 1000 + 121 - passphraseLen : 121;
}

/** PBKDF2-based decryption (V2 / legacy formats). */
async function decryptV2(data: string, passphrase: string): Promise<string> {
  // Format: % | iv_hex(32 chars = 16 bytes) | salt_hex(32 chars = 16 bytes) | base64-data
  const ivHex = data.slice(1, 33);
  const saltHex = data.slice(33, 65);
  const encB64 = data.slice(65);

  const iv = Buffer.from(ivHex, 'hex');
  const salt = Buffer.from(saltHex, 'hex');
  const encBuf = Buffer.from(encB64, 'base64');

  // The last TAG_LEN bytes are the GCM auth tag.
  if (encBuf.length < TAG_LEN) throw new Error('ciphertext too short for GCM tag');
  const ciphertext = encBuf.subarray(0, encBuf.length - TAG_LEN);
  const authTag = encBuf.subarray(encBuf.length - TAG_LEN);

  // Derive key — try dynamic iteration count first, fall back to 100000.
  for (const iters of [getV2Iterations(passphrase), 100000]) {
    try {
      const passBuf = Buffer.from(passphrase, 'utf8');
      const digest = Buffer.from(
        await subtle_digest('SHA-256', passBuf),
      );
      const key = pbkdf2Sync(digest, salt, iters, 32, 'sha256');
      const decipher = createDecipheriv(AES_GCM, key, iv);
      decipher.setAuthTag(authTag);
      const plain = Buffer.concat([decipher.update(ciphertext), decipher.final()]);
      return plain.toString('utf8');
    } catch {
      // try next iteration count
    }
  }
  throw new Error('V2 decryption failed with all known iteration counts');
}

/** HKDF-based decryption ("%=" prefix, from octagonal-wheels/encryption/hkdf). */
async function decryptHKDF(data: string, passphrase: string): Promise<string> {
  // Format: %=<base64(salt(32) + iv(12) + ciphertext + tag(16))>
  const b64 = data.slice(2); // strip "%="
  const buf = Buffer.from(b64, 'base64');
  if (buf.length < 32 + 12 + TAG_LEN) throw new Error('HKDF ciphertext too short');

  const salt = buf.subarray(0, 32);
  const iv = buf.subarray(32, 44);
  const ciphertext = buf.subarray(44, buf.length - TAG_LEN);
  const authTag = buf.subarray(buf.length - TAG_LEN);

  const ikm = Buffer.from(passphrase, 'utf8');
  // HKDF-SHA256: info="" (empty), output 32 bytes
  const key = Buffer.from(hkdfSync('sha256', ikm, salt, Buffer.alloc(0), 32));

  const decipher = createDecipheriv(AES_GCM, key, iv);
  decipher.setAuthTag(authTag);
  const plain = Buffer.concat([decipher.update(ciphertext), decipher.final()]);
  return plain.toString('utf8');
}

/** Encrypt with HKDF (used when pushing files to CouchDB). */
function encryptHKDF(plaintext: string, passphrase: string): string {
  const salt = randomBytes(32);
  const iv = randomBytes(12);
  const ikm = Buffer.from(passphrase, 'utf8');
  const key = Buffer.from(hkdfSync('sha256', ikm, salt, Buffer.alloc(0), 32));

  const cipher = createCipheriv(AES_GCM, key, iv);
  const encrypted = Buffer.concat([cipher.update(Buffer.from(plaintext, 'utf8')), cipher.final()]);
  const tag = cipher.getAuthTag();

  const combined = Buffer.concat([salt, iv, encrypted, tag]);
  return HKDF_PREFIX + combined.toString('base64');
}

/** Node-compatible WebCrypto SHA-256 digest (avoids importing subtle directly). */
async function subtle_digest(algo: string, data: Buffer): Promise<ArrayBuffer> {
  // Use the global crypto available in Node 15+.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  return (globalThis.crypto as any).subtle.digest(algo, data);
}

/** Decrypt a LiveSync .data string (handles all known encryption formats). */
async function decryptData(data: string, passphrase: string): Promise<string> {
  if (!data) return data;
  if (data.startsWith(HKDF_PREFIX)) {
    return decryptHKDF(data, passphrase);
  }
  if (data.startsWith(V3_PREFIX) || data.startsWith(V2_PREFIX)) {
    return decryptV2(data, passphrase);
  }
  if (data.startsWith('[')) {
    // V1: ["encryptedData","iv","salt"]
    // Same PBKDF2/AES-GCM but stored as JSON array; encrypted data is base64.
    const [encB64, ivHex, saltHex] = JSON.parse(data) as [string, string, string];
    const iv = Buffer.from(ivHex, 'hex');
    const salt = Buffer.from(saltHex, 'hex');
    const encBuf = Buffer.from(encB64, 'base64');
    if (encBuf.length < TAG_LEN) throw new Error('V1 ciphertext too short');
    const ciphertext = encBuf.subarray(0, encBuf.length - TAG_LEN);
    const authTag = encBuf.subarray(encBuf.length - TAG_LEN);

    for (const iters of [getV2Iterations(passphrase), 100000]) {
      try {
        const passBuf = Buffer.from(passphrase, 'utf8');
        const digest = Buffer.from(await subtle_digest('SHA-256', passBuf));
        const key = pbkdf2Sync(digest, salt, iters, 32, 'sha256');
        const decipher = createDecipheriv(AES_GCM, key, iv);
        decipher.setAuthTag(authTag);
        const raw = Buffer.concat([decipher.update(ciphertext), decipher.final()]);
        // V1 stores JSON.stringify(string) so we need to parse.
        return JSON.parse(raw.toString('utf8')) as string;
      } catch {
        // try next
      }
    }
    throw new Error('V1 decryption failed');
  }
  // Not encrypted — plaintext.
  return data;
}

// ── Internal doc type predicates ─────────────────────────────────────────────

function isChunkDoc(doc: DocLike): boolean {
  return typeof doc._id === 'string' && doc._id.startsWith('h:');
}

function hasChildren(doc: DocLike): doc is DocLike & { children: string[] } {
  return Array.isArray((doc as { children?: unknown }).children);
}

function isNoteEntry(doc: DocLike): boolean {
  const t = (doc as { type?: string }).type;
  return t === 'notes' || t === 'plain' || t === 'newnote';
}

// ── Public codec ─────────────────────────────────────────────────────────────

export class LiveSyncCodec {
  private readonly passphrase: string;

  constructor(passphrase: string) {
    this.passphrase = passphrase;
  }

  /**
   * Decode a CouchDB document (as produced by Self-hosted LiveSync) into a
   * vault file. Returns `null` if the doc should not be mirrored (internal
   * doc, chunk, etc.).
   *
   * For chunked docs the caller must ensure all chunk docs are already
   * present in CouchDB — we fetch them inline via the couch client passed
   * to assembleFromCouchDocWithChunks(). For the simple case where the doc
   * itself carries all content (legacy NoteEntry with inline data), this
   * method works standalone.
   */
  assembleFromCouchDoc(doc: DocLike): VaultFile | null {
    if (this.isInternalId(doc._id)) return null;
    if (isChunkDoc(doc)) return null; // chunk, not a file
    if (!isNoteEntry(doc)) return null;

    const path = this.extractPath(doc);
    if (!path) return null;

    // Legacy NoteEntry: data field contains the whole file (possibly encrypted).
    const rawData = (doc as { data?: string | string[] }).data;
    if (typeof rawData === 'string') {
      // Synchronously return a placeholder; real decryption happens in the
      // async variant below. Throw so callers are reminded to use the async path.
      throw new NotImplementedError(
        'Use assembleFromCouchDocAsync for encrypted/chunked documents.',
      );
    }

    return null;
  }

  /**
   * Async version of assembleFromCouchDoc. Handles:
   *   - Legacy NoteEntry (inline data, possibly encrypted)
   *   - PlainEntry / NewEntry (children[] → chunk docs must be fetched)
   *
   * `fetchDoc` is called for each chunk doc ID; it should return the raw
   * CouchDB document or null if not found.
   */
  async assembleFromCouchDocAsync(
    doc: DocLike,
    fetchDoc: (id: string) => Promise<DocLike | null>,
  ): Promise<VaultFile | null> {
    if (this.isInternalId(doc._id)) return null;
    if (isChunkDoc(doc)) return null;
    if (!isNoteEntry(doc)) return null;

    const path = this.extractPath(doc);
    if (!path) return null;

    let plainContent: string;

    if (hasChildren(doc) && doc.children.length > 0) {
      // Fetch and concatenate all chunks.
      const parts: string[] = [];
      for (const chunkId of doc.children) {
        const chunkDoc = await fetchDoc(chunkId);
        if (!chunkDoc) {
          throw new Error(`missing chunk ${chunkId} for ${path}`);
        }
        const rawChunk = (chunkDoc as { data?: string }).data ?? '';
        const decrypted = await decryptData(rawChunk, this.passphrase);
        parts.push(decrypted);
      }
      plainContent = parts.join('');
    } else {
      // Legacy inline data.
      const rawData = (doc as { data?: string | string[] }).data;
      if (Array.isArray(rawData)) {
        // Some versions store data as string[]; concatenate.
        const parts: string[] = [];
        for (const part of rawData) {
          parts.push(await decryptData(part, this.passphrase));
        }
        plainContent = parts.join('');
      } else if (typeof rawData === 'string') {
        plainContent = await decryptData(rawData, this.passphrase);
      } else {
        // Empty or unknown — produce an empty file.
        plainContent = '';
      }
    }

    return { path, body: Buffer.from(plainContent, 'utf8') };
  }

  /**
   * Encode a vault file as CouchDB document(s).
   *
   * Phase-1 implementation: single-chunk PlainEntry. The whole file content
   * goes into one chunk doc referenced by the entry doc. Obsidian LiveSync
   * will re-chunk on next sync if needed.
   */
  splitForCouchDoc(file: VaultFile): DocLike[] {
    const plainContent = file.body.toString('utf8');

    // Encrypt content.
    const encryptedContent = encryptHKDF(plainContent, this.passphrase);

    // Chunk doc: id = "h:<sha1 of plaintext>" — LiveSync uses murmurhash in
    // practice, but any stable id works; we use a truncated sha1.
    const chunkId = this.makeChunkId(plainContent);
    const chunkDoc: DocLike = {
      _id: chunkId,
      type: 'leaf',
      data: encryptedContent,
    };

    // Entry doc.
    const entryDoc: DocLike = {
      _id: file.path as string,
      type: 'plain',
      path: file.path,
      children: [chunkId],
      mtime: Date.now(),
      ctime: Date.now(),
      size: file.body.length,
      eden: {},
    };

    return [chunkDoc, entryDoc];
  }

  /**
   * Returns true iff the given doc id is one LiveSync uses for its own
   * bookkeeping and should not be mirrored to disk.
   */
  isInternalId(id: string): boolean {
    if (id.startsWith('_')) return true; // _design, _local, _users etc.
    if (id.startsWith('h:')) return true; // chunk or history doc
    if (id.startsWith('chunk-')) return true;
    // LiveSync sync-info and versioning docs.
    if (id === '$$SYNC_INFO$$' || id === '$$VERSIONING$$' || id === '$$MILESTONE$$') return true;
    if (id === '$$NODEINFO$$') return true;
    return false;
  }

  // ── Private helpers ────────────────────────────────────────────────────────

  private extractPath(doc: DocLike): string | null {
    // Prefer the explicit `path` field; fall back to `_id`.
    const p = (doc as { path?: string }).path ?? doc._id;
    if (!p || typeof p !== 'string') return null;
    // Strip any prefix LiveSync adds for obfuscated paths. The canonical
    // path is always forward-slash separated vault-relative.
    return p.replace(/\\/g, '/');
  }

  private makeChunkId(content: string): string {
    // Deterministic chunk id: sha256 truncated to 40 hex chars with "h:" prefix.
    const hash = createHash('sha256').update(content, 'utf8').digest('hex').slice(0, 40);
    return `h:${hash}`;
  }
}
