# apps/tidy — tidy-worker (Go)

AI 整理 worker。读 inbox 文件 → 检索上下文 → 调 LLM → diff 校验 → 写 notes/。

## 关键:diff 校验

`internal/safediff/` 是「不改我记的内容」的硬保障:

1. 取 LLM 返回的 `body_with_links`
2. 把所有 `[[…]]` 块拆成「目标文本」(去掉 wikilink 包裹)
3. 与原文比对——如果除了「某些位置原本是 X,现在变成了 X 但被 `[[X]]` 或 `[[X|alias]]` 包裹」之外有任何字符差异,**拒绝**这次改动。
4. 拒绝后回退最小整理:只写 frontmatter,正文逐字保持原样。

## 待实现

- `cmd/tidy/main.go` — 单次 job,接 path 参数(或 --all)
- `internal/llm/` — OpenAI 兼容客户端
- `internal/safediff/` — 校验器(关键)
- `internal/qdrant/` — 检索 top-K
- `internal/atlas/` — MOC 维护
- `Dockerfile` — 长驻服务,通过 IPC / HTTP 接收 api 的触发(或者每次 docker exec)
- `go.mod` / `go.sum`
