#!/usr/bin/env node
// knowLib skill CLI. No external deps.
// Reads server_url and api_token from ../config.json.

const fs = require('fs');
const path = require('path');

const CONFIG = JSON.parse(
  fs.readFileSync(path.join(__dirname, '..', 'config.json'), 'utf8')
);
const BASE = CONFIG.server_url.replace(/\/+$/, '');
const TOKEN = CONFIG.api_token;

if (!BASE || BASE.includes('{{') || !TOKEN || TOKEN.includes('{{')) {
  console.error('knowLib skill: config.json is not filled in (still has template placeholders).');
  console.error('Download a personalized bundle from your knowLib /settings page.');
  process.exit(2);
}

async function call(method, urlPath, { query = {}, body = null } = {}) {
  const url = new URL(BASE + urlPath);
  for (const [k, v] of Object.entries(query)) {
    if (v != null) url.searchParams.set(k, String(v));
  }
  const init = {
    method,
    headers: {
      'Authorization': `Bearer ${TOKEN}`,
      'Accept': 'application/json',
    },
  };
  if (body != null) {
    init.headers['Content-Type'] = 'application/json';
    init.body = JSON.stringify(body);
  }
  const res = await fetch(url, init);
  const text = await res.text();
  if (!res.ok) {
    console.error(`knowLib API error ${res.status}: ${text}`);
    process.exit(1);
  }
  try { return JSON.parse(text); } catch { return text; }
}

function flag(args, name, fallback) {
  const i = args.indexOf(`--${name}`);
  if (i < 0) return fallback;
  return args[i + 1];
}

async function main() {
  const [, , cmd, ...rest] = process.argv;
  switch (cmd) {
    case 'search': {
      const q = rest.find(a => !a.startsWith('--'));
      const k = parseInt(flag(rest, 'k', '8'), 10);
      if (!q) { console.error('usage: knowlib search "<query>" [--k 8]'); process.exit(2); }
      const out = await call('GET', '/api/search', { query: { q, k } });
      console.log(JSON.stringify(out, null, 2));
      break;
    }
    case 'get': {
      const p = rest.find(a => !a.startsWith('--'));
      if (!p) { console.error('usage: knowlib get <path>'); process.exit(2); }
      const out = await call('GET', '/api/note', { query: { path: p } });
      console.log(JSON.stringify(out, null, 2));
      break;
    }
    case 'list': {
      const category = flag(rest, 'category');
      const out = await call('GET', '/api/list', { query: { category } });
      console.log(JSON.stringify(out, null, 2));
      break;
    }
    case 'capture': {
      const content = rest.find(a => !a.startsWith('--'));
      const source = flag(rest, 'source', 'ai');
      if (!content) { console.error('usage: knowlib capture "<content>" [--source ai]'); process.exit(2); }
      const out = await call('POST', '/api/capture', { body: { content, source } });
      console.log(JSON.stringify(out, null, 2));
      break;
    }
    default:
      console.error('commands: search | get | list | capture');
      process.exit(2);
  }
}

main().catch(err => {
  console.error(err);
  process.exit(1);
});
