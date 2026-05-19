"""Smoke tests for chunker behaviour. Run with `pytest -q apps/embedder`.

These do NOT exercise the embed or qdrant code paths — those need real services.
"""

from __future__ import annotations

import sys
from pathlib import Path

# Make `embedder` importable as a package when pytest runs from repo root.
sys.path.insert(0, str(Path(__file__).resolve().parents[1].parent))

from embedder.chunker import chunk_markdown, count_tokens  # noqa: E402


def test_no_headings_yields_one_or_more_chunks():
    body = "Just a few sentences. No headings.\n\nAnother paragraph here."
    chunks = chunk_markdown(body, max_tokens=800, overlap_tokens=0)
    assert len(chunks) == 1
    assert "another paragraph" in chunks[0].content.lower()
    assert chunks[0].heading_path == []


def test_h2_split_creates_separate_chunks():
    body = (
        "## 一阶 ODE\n"
        "线性方程的形式 dy/dx + P(x)y = Q(x)。\n\n"
        "## 二阶 ODE\n"
        "常系数齐次方程 y'' + ay' + by = 0。\n"
    )
    chunks = chunk_markdown(body, max_tokens=800, overlap_tokens=0)
    paths = [c.heading_path for c in chunks]
    assert ["一阶 ODE"] in paths
    assert ["二阶 ODE"] in paths


def test_long_section_splits_within_section():
    para = "这是一个段落。" * 200
    body = f"## 长章节\n{para}\n\n{para}\n"
    chunks = chunk_markdown(body, max_tokens=200, overlap_tokens=20)
    # Should produce more than one chunk, all carrying the same heading_path
    assert len(chunks) >= 2
    assert all(c.heading_path == ["长章节"] for c in chunks)
    for c in chunks:
        # Allow a single chunk to slightly exceed cap if a single unit was huge,
        # but most should be near the cap.
        assert count_tokens(c.content) <= 250
