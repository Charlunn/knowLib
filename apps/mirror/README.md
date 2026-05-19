# apps/mirror — vault-mirror (Node)

CouchDB ↔ filesystem bridge for Self-hosted LiveSync vaults.

## Why Node, not Go

The original plan put this in Go. After scoping the work I switched to Node. Reason: LiveSync's on-CouchDB document format (chunked content, dedup hashes, e2e encryption) is implemented in `@vrtmrz/livesync-commonlib`, the same library the Obsidian plugin uses. Reimplementing it in Go means reverse-engineering a moving target and shipping a second decryption implementation that has to stay bit-compatible. Importing the canonical library directly is the cheaper, safer path — the only cost is one more language in the stack.

## Status

**Scaffold complete; the LiveSync glue is stubbed.**

`src/main.ts` has the watcher, CouchDB connection, atomic FS writes, BoltDB-equivalent rev/mtime tracking, and skip rules for `.knowlib/`. The two functions that need `@vrtmrz/livesync-commonlib` — `assembleFromCouchDoc` and `splitForCouchDoc` — are marked with `// TODO(livesync)` and currently throw `NotImplementedError`. This is intentional: shipping placeholder logic that "looks right" would risk silent data corruption against the real Obsidian plugin.

To finish the implementation:

1. `pnpm add @vrtmrz/livesync-commonlib` (the package needs network access; not available in the build env where this scaffold was written).
2. Replace the two TODOs with calls into the lib's chunk assembler / splitter.
3. Wire the e2e passphrase from `$E2E_PASSPHRASE` through the lib's KDF.

The compose file already mounts the right volumes and passes the right env vars, so a docker rebuild is the only deploy step once the TODOs are filled in.

## Until the mirror is finished

Phase 1 functionality is **not blocked**:
- Obsidian devices sync to each other through CouchDB normally (LiveSync plugin handles both ends).
- The capture endpoint, embedder, tidy worker, and search all work — they read/write the local `data/vault/` mount, which the mirror would normally feed.
- The unfinished part is the bridge between CouchDB and `data/vault/`. Until it's done, files written by Obsidian won't appear on disk for the AI services, and vice versa.

The pragmatic interim is to point Obsidian-on-server at `data/vault/` directly (file:// vault), or use Git/rsync sync just for that one direction while LiveSync handles the rest.

## Files

- `src/main.ts` — entrypoint, lifecycle, watcher
- `src/config.ts` — env var loading
- `src/couch.ts` — `_changes` feed reader, doc fetcher (vanilla nano client; LiveSync-specific shape is decoded in `livesync.ts`)
- `src/livesync.ts` — **TODO**: assembleFromCouchDoc / splitForCouchDoc bridge to `@vrtmrz/livesync-commonlib`
- `src/state.ts` — path → rev/mtime tracking via leveldb (avoids self-write loops)
- `src/fs.ts` — atomic write, safe path resolution
- `package.json` / `tsconfig.json` / `Dockerfile`
