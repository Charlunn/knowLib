// Atomic-write filesystem helpers. Mirrors the safety contract of the api/tidy
// vault packages: every write is temp-file + fsync + rename so the filesystem
// watcher (embedder) never observes a partial document.

import { promises as fs } from 'fs';
import { dirname, join, normalize, resolve, sep } from 'path';

export class PathEscapeError extends Error {}

export class SafeFS {
  constructor(private readonly root: string) {}

  resolve(rel: string): string {
    const normalised = rel.replace(/\\/g, '/');
    if (normalised === '' || normalised === '.' || normalised === '..') {
      throw new PathEscapeError(`empty or trivial path: ${rel}`);
    }
    if (normalised.startsWith('/') || /^[A-Za-z]:/.test(normalised)) {
      throw new PathEscapeError(`absolute path rejected: ${rel}`);
    }
    for (const seg of normalised.split('/')) {
      if (seg === '..') throw new PathEscapeError(`'..' segment rejected: ${rel}`);
    }
    const abs = resolve(this.root, normalize(normalised));
    const rootAbs = resolve(this.root);
    if (!(abs === rootAbs || abs.startsWith(rootAbs + sep))) {
      throw new PathEscapeError(`path escapes root: ${rel}`);
    }
    return abs;
  }

  async writeAtomic(rel: string, body: Buffer | string): Promise<void> {
    const abs = this.resolve(rel);
    await fs.mkdir(dirname(abs), { recursive: true });
    const tmp = abs + '.tmp.' + process.pid + '.' + Date.now();
    const data = typeof body === 'string' ? Buffer.from(body, 'utf8') : body;
    await fs.writeFile(tmp, data);
    try {
      const fh = await fs.open(tmp, 'r+');
      try {
        await fh.sync();
      } finally {
        await fh.close();
      }
      await fs.rename(tmp, abs);
    } catch (err) {
      await fs.rm(tmp, { force: true }).catch(() => {});
      throw err;
    }
  }

  async remove(rel: string): Promise<void> {
    const abs = this.resolve(rel);
    await fs.rm(abs, { force: true });
  }

  async read(rel: string): Promise<Buffer> {
    return fs.readFile(this.resolve(rel));
  }

  async statMtime(rel: string): Promise<number | null> {
    try {
      const s = await fs.stat(this.resolve(rel));
      return s.mtimeMs;
    } catch (err: unknown) {
      if ((err as NodeJS.ErrnoException).code === 'ENOENT') return null;
      throw err;
    }
  }

  joinForVault(parts: string[]): string {
    return parts.map((p) => p.replace(/\\/g, '/')).join('/');
  }

  static normaliseVaultPath(rel: string): string {
    return rel.replace(/\\/g, '/');
  }
}

export function isUnderHidden(relPath: string, hiddenDir: string): boolean {
  const segs = relPath.split('/');
  return segs.includes(hiddenDir) || segs.some((s) => s.startsWith('.'));
}

// Re-export join for callers that want vault-relative path arithmetic without
// pulling node:path in their files (and accidentally getting Windows separators).
export function vaultJoin(...parts: string[]): string {
  return join(...parts).replace(/\\/g, '/');
}
