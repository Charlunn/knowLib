"""Markdown chunking by H2/H3 headings with token-cap fallback.

Strategy:
  1. Parse frontmatter once at the top (handled by caller; we only see the body).
  2. Walk the body; treat fenced code blocks as atomic so we never split inside
     a ``` ... ``` region. A fenced block bigger than the cap is allowed to
     exceed it rather than corrupt the code.
  3. Split at H2/H3 boundaries first. If a section is still too big, fall back
     to a token-cap sliding window with overlap so retrieval still works.
  4. Files with no H2/H3 at all become token-cap chunks across the whole body.

Each emitted chunk carries `heading_path` (e.g. ["前言", "动机"]) so the
retrieval side can show breadcrumbs without re-parsing the file.
"""

from __future__ import annotations

import re
from dataclasses import dataclass, field

import tiktoken

# Single shared encoder. cl100k_base is OpenAI's tokenizer; using it keeps token
# counts comparable with the rest of the stack (LLM context budgets, etc.).
_ENCODER = tiktoken.get_encoding("cl100k_base")


def count_tokens(text: str) -> int:
    return len(_ENCODER.encode(text))


@dataclass
class Chunk:
    content: str
    heading_path: list[str] = field(default_factory=list)
    chunk_index: int = 0


# Match ATX H2/H3 only. We deliberately ignore H1 because in markdown notes the
# H1 is usually the file title (already in the payload) — splitting on it would
# emit a single giant chunk for the whole file.
_HEADING_RE = re.compile(r"^(#{2,3})\s+(.+?)\s*$")
_FENCE_RE = re.compile(r"^(```|~~~)")


@dataclass
class _Section:
    """A heading-bounded section before token-cap splitting."""
    heading_path: list[str]
    body: str


def _split_into_sections(text: str) -> list[_Section]:
    """Walk lines and group them under their nearest H2/H3 heading.

    Code fences are preserved as-is; headings inside fences are ignored.
    """
    lines = text.splitlines(keepends=True)
    sections: list[_Section] = []
    # Stack of (level, title) so H3 inherits its parent H2 in the heading path.
    heading_stack: list[tuple[int, str]] = []
    current_path: list[str] = []
    buf: list[str] = []
    in_fence = False

    def flush() -> None:
        if buf:
            sections.append(_Section(heading_path=list(current_path), body="".join(buf)))
            buf.clear()

    for line in lines:
        if _FENCE_RE.match(line):
            in_fence = not in_fence
            buf.append(line)
            continue

        if not in_fence:
            m = _HEADING_RE.match(line)
            if m:
                level = len(m.group(1))
                title = m.group(2).strip()
                # New heading starts a new section; flush whatever was buffered
                # under the *previous* heading first.
                flush()
                # Pop the stack so a sibling H2 replaces a previous H2.
                while heading_stack and heading_stack[-1][0] >= level:
                    heading_stack.pop()
                heading_stack.append((level, title))
                current_path = [t for _, t in heading_stack]
                # Include the heading line itself in the new section so chunks
                # are self-contained when read in isolation.
                buf.append(line)
                continue

        buf.append(line)

    flush()
    return sections


def _split_body_atomic(body: str) -> list[str]:
    """Split a section body into atomic units: fenced blocks + paragraphs.

    Fenced blocks are kept whole (one unit). Outside fences we split on blank
    lines so paragraphs stay intact for the token-cap packer.
    """
    units: list[str] = []
    lines = body.splitlines(keepends=True)
    i = 0
    cur: list[str] = []

    def flush_cur() -> None:
        if cur:
            text = "".join(cur).strip("\n")
            if text:
                units.append(text)
            cur.clear()

    while i < len(lines):
        line = lines[i]
        if _FENCE_RE.match(line):
            # Close out any plain text first, then capture the fenced block whole.
            flush_cur()
            fence_lines = [line]
            i += 1
            while i < len(lines):
                fence_lines.append(lines[i])
                if _FENCE_RE.match(lines[i]):
                    i += 1
                    break
                i += 1
            units.append("".join(fence_lines).rstrip("\n"))
            continue

        if line.strip() == "":
            # Blank line: paragraph boundary.
            flush_cur()
            i += 1
            continue

        cur.append(line)
        i += 1

    flush_cur()
    return units


def _pack_units(
    units: list[str],
    max_tokens: int,
    overlap_tokens: int,
) -> list[str]:
    """Greedy pack atomic units into chunks <= max_tokens with token overlap.

    A unit larger than max_tokens (typically a giant code fence) is emitted as
    its own oversized chunk rather than corrupted by a mid-fence split.
    """
    chunks: list[str] = []
    cur: list[str] = []
    cur_tokens = 0

    def flush() -> None:
        nonlocal cur, cur_tokens
        if cur:
            chunks.append("\n\n".join(cur))
            cur = []
            cur_tokens = 0

    for unit in units:
        u_tokens = count_tokens(unit)

        # Oversized atomic unit: flush current buffer, emit unit as its own chunk.
        if u_tokens > max_tokens:
            flush()
            chunks.append(unit)
            continue

        if cur_tokens + u_tokens <= max_tokens:
            cur.append(unit)
            cur_tokens += u_tokens
            continue

        # Doesn't fit: emit current chunk, then seed the next one with the tail
        # of the previous chunk for `overlap_tokens` worth of context. Overlap
        # is computed at the unit boundary so we never split a paragraph.
        flush()
        if overlap_tokens > 0 and chunks:
            tail = _take_tail_units(chunks[-1], overlap_tokens)
            if tail:
                cur.append(tail)
                cur_tokens += count_tokens(tail)
        cur.append(unit)
        cur_tokens += u_tokens

    flush()
    return chunks


def _take_tail_units(chunk_text: str, overlap_tokens: int) -> str:
    """Return the tail paragraphs of `chunk_text` that fit in overlap_tokens."""
    paragraphs = chunk_text.split("\n\n")
    tail: list[str] = []
    total = 0
    for p in reversed(paragraphs):
        t = count_tokens(p)
        if total + t > overlap_tokens:
            break
        tail.insert(0, p)
        total += t
    return "\n\n".join(tail)


def chunk_markdown(
    body: str,
    max_tokens: int = 800,
    overlap_tokens: int = 80,
) -> list[Chunk]:
    """Public API: turn a markdown body into Chunk objects.

    Empty / whitespace-only input yields an empty list (caller should skip).
    """
    if not body or not body.strip():
        return []

    sections = _split_into_sections(body)

    # Files with no H2/H3 at all collapse to one synthetic section so the
    # token-cap packer still produces sensible chunks.
    if not sections or all(not s.heading_path for s in sections):
        units = _split_body_atomic(body)
        packed = _pack_units(units, max_tokens, overlap_tokens)
        return [
            Chunk(content=c, heading_path=[], chunk_index=i)
            for i, c in enumerate(packed)
        ]

    chunks: list[Chunk] = []
    idx = 0
    for sec in sections:
        units = _split_body_atomic(sec.body)
        packed = _pack_units(units, max_tokens, overlap_tokens)
        for c in packed:
            chunks.append(Chunk(content=c, heading_path=list(sec.heading_path), chunk_index=idx))
            idx += 1
    return chunks
