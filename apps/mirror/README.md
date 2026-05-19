# apps/mirror — vault-mirror (Node)

CouchDB ↔ filesystem bridge for Self-hosted LiveSync vaults.

## Why Node, not Go

The original plan put this in Go. After scoping the work I switched to Node. Reason: LiveSync's on-CouchDB document format (chunked content, dedup hashes, e2e encryption) is implemented in `@vrtmrz/livesync-commonlib`, the same library the Obsidian plugin uses. Reimplementing it in Go means reverse-engineering a moving target and shipping a second decryption implementation that has to stay bit-compatible. Importing the canonical library directly is the cheaper, safer path — the only cost is one more language in the stack.

## Status

**Implemented.** The LiveSync codec (`src/livesync.ts`) handles all known encryption formats used by Self-hosted LiveSync:

- **V2** (`%` prefix): AES-GCM + PBKDF2-SHA256, dynamic iteration count
- **V3** (`%~` prefix): same algorithm, V3 variant
- **HKDF** (`%=` prefix): AES-GCM + HKDF-SHA256, ephemeral salt
- **V1** (`[` prefix): legacy JSON-array format
- **Plaintext**: no prefix

The implementation uses Node.js built-in `crypto` module only (no external dependencies), producing bit-identical output to the browser WebCrypto API used by the original Obsidian plugin.

For write path (fs → CouchDB), files are encoded as single-chunk `PlainEntry` documents with HKDF encryption. Obsidian LiveSync will re-chunk on next open if needed.

The full service stack is running: watcher, CouchDB connection, atomic FS writes, LevelDB rev/mtime tracking, and skip rules for `.knowlib/`.

## Files

- `src/main.ts` — entrypoint, lifecycle, watcher
- `src/config.ts` — env var loading
- `src/couch.ts` — `_changes` feed reader, doc fetcher (vanilla nano client; LiveSync-specific shape is decoded in `livesync.ts`)
- `src/livesync.ts` — **TODO**: assembleFromCouchDoc / splitForCouchDoc bridge to `@vrtmrz/livesync-commonlib`
- `src/state.ts` — path → rev/mtime tracking via leveldb (avoids self-write loops)
- `src/fs.ts` — atomic write, safe path resolution
- `package.json` / `tsconfig.json` / `Dockerfile`
