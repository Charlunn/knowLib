// Loads env vars into a typed, validated config. Defaults match
// docker-compose.yml so local dev (without compose) still works.

export interface Config {
  couchUrl: string;
  couchDb: string;
  e2ePassphrase: string;
  vaultPath: string;
  knowlibDir: string;
  stateDir: string;
}

function envOr(key: string, def?: string): string {
  const v = process.env[key];
  if (v && v.trim() !== '') return v;
  if (def !== undefined) return def;
  throw new Error(`required env var ${key} is missing`);
}

export function loadConfig(): Config {
  const couchUrl = envOr('COUCHDB_URL');
  const couchDb = envOr('COUCHDB_DB', 'obsidian-vault');
  // E2E_PASSPHRASE is required because LiveSync clients write encrypted
  // content; without the passphrase we can't decode any document.
  const e2ePassphrase = envOr('E2E_PASSPHRASE');
  const vaultPath = envOr('VAULT_PATH', '/data/vault');
  const knowlibDir = envOr('KNOWLIB_DIR', '.knowlib');
  const stateDir = envOr('MIRROR_STATE_DIR', '/state');
  return { couchUrl, couchDb, e2ePassphrase, vaultPath, knowlibDir, stateDir };
}
