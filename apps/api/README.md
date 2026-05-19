# apps/api — knowlib-api (Go)

knowLib 主服务。HTTP REST + MCP server + TOTP 登录。

## 路由概览

| 方法 | 路径 | 鉴权 | 作用 |
|---|---|---|---|
| POST | `/api/auth/login` | 无 | TOTP 6 位码 → JWT |
| POST | `/api/capture` | JWT/Token | 写 `inbox/<ts>-<hash>.md` |
| GET  | `/api/inbox` | JWT/Token | 列 inbox 文件 |
| POST | `/api/tidy` | JWT/Token | 触发 tidy-worker(单篇或全量)|
| GET  | `/api/search?q=&k=` | JWT/Token | 向量检索 |
| GET  | `/api/note?path=` | JWT/Token | 取笔记全文 |
| GET  | `/api/list?category=` | JWT/Token | 列分类下笔记 |
| GET/PUT | `/api/settings` | JWT | 读写配置 |
| GET/PUT | `/api/tokens` | JWT | API token 管理 |
| GET  | `/api/skill-bundle.zip` | JWT | 下载注入个人配置的 skill 包 |
| GET/POST | `/mcp` | Token | MCP server (SSE + JSON-RPC) |

## 待实现

- ~~`cmd/api/main.go` — entrypoint~~ ✅
- ~~`internal/auth/` — TOTP + JWT~~ ✅
- ~~`internal/handlers/` — REST handlers~~ ✅
- ~~`internal/mcp/` — MCP server~~ ✅
- ~~`internal/store/` — 设置存储(JSON 文件 / BoltDB)~~ ✅
- ~~`internal/vault/` — 文件读写,封装 vault 路径访问~~ ✅
- ~~`internal/llm/` — OpenAI 兼容客户端~~ ✅
- ~~`internal/qdrant/` — Qdrant 客户端~~ ✅
- ~~`Dockerfile` — 多阶段构建~~ ✅
- ~~`go.mod` / `go.sum`~~ ✅
