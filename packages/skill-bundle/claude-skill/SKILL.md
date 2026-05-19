---
name: knowlib
description: Search and capture notes in the user's personal knowLib knowledge base. Use this when answering questions that benefit from the user's own past notes, when the user says "remember this" or "save this", or when asked to find what they know about a topic.
---

# knowLib skill

This Claude Code skill connects to the user's self-hosted knowLib instance. It exposes four operations through the REST API.

The server URL and API token are baked into `config.json` (next to this file) — the bundle downloader on the knowLib settings page substitutes them for you. If you got this skill from somewhere else, edit `config.json`.

## When to use

- The user asks "what do I know about X" / "do I have notes on Y" / "based on my notes" → call `search`.
- The user says "remember this" / "save this idea" / "add to my notes" → call `capture`.
- You want to ground an answer with citations from the user's own notes → call `search`, then `get` the most relevant hits.

## Operations

### search

```
node ./bin/knowlib.js search "<query>" [--k 8]
```

Returns top-K notes ranked by semantic similarity. Output is JSON: `[{path, title, snippet, score}, ...]`.

### get

```
node ./bin/knowlib.js get <vault-relative-path>
```

Returns `{path, title, frontmatter, body}` for a single note.

### list

```
node ./bin/knowlib.js list [--category <prefix>]
```

Returns a list of notes (paths + titles) under the given category prefix.

### capture

```
node ./bin/knowlib.js capture "<content>" [--source ai]
```

Writes the content to `inbox/<timestamp>-<hash>.md` in the user's vault. Returns `{path}`. The tidy worker will pick it up later.

## Notes

- All paths in the API are vault-relative (forward slashes).
- The token is a long-lived API token, not a session JWT. Don't share it.
- If a request returns 401, the token has been revoked — tell the user to re-download the skill bundle.
