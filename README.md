# knowLib

自托管 AI 增强 Obsidian 知识库。任何端随手写,AI 帮你整理分类、加 wikilinks、维护知识图谱,**不改你写的内容**。同时通过 MCP / REST / OpenAI tool / Skill 包暴露给任何 AI 客户端,做你自己的 AI 知识底座。

## 设计原则

- **写作自由,查阅严谨**:写笔记零仪式感,查笔记像图书馆。
- **AI 不动你的字**:AI 只在 `inbox/` 里整理,处理后归位 `notes/<分类>/`,只加 frontmatter / wikilinks / 移动文件。代码层面 diff 校验,LLM 不能跨界。
- **多端实时同步**:Obsidian 官方应用 (PC / Mac / iOS / Android) 通过 Self-hosted LiveSync。
- **任意 AI 客户端可用**:一份 skill 包,Claude Desktop / Cursor / Cherry Studio / 任何 OpenAI 兼容客户端都能挂。
- **简单认证**:TOTP-only,扫码一次,以后只输 6 位。

## 架构

```
Obsidian 端 ──LiveSync──┐                 ┌── Web PWA (快捕/监控/触发)
                        ↓                 ↓
              Caddy (Let's Encrypt + 反代)
                        ↓
   ┌────────────┬───────────┬────────────┐
   │  CouchDB   │ knowlib-  │  Next.js    │
   │ (LiveSync  │ api (Go)  │   web       │
   │  后端)     │ + MCP     │             │
   └─────┬──────┴────┬──────┴─────────────┘
         │           │
   vault-mirror   tidy-worker (按需)
   (Go,双向)      (Go,AI 整理)
         │           │
         ↓           ↓
   data/vault/  ←── fsnotify ──→  embedder (Python) ──→ Qdrant
```

数据流向两条:
1. **写入**:任何端 → `vault/inbox/<时间戳>-<slug>.md` → CouchDB → mirror 落到文件 → embedder 索引。
2. **AI 整理**:Web 触发 → tidy-worker 读 inbox → Qdrant 检索上下文 → LLM 生成 frontmatter + wikilinks → diff 校验 → 写入 `notes/<分类>/<标题>.md` → 删除 inbox 原文件。

## 快速开始

```bash
# 1. 克隆 + 配置
git clone <repo> knowLib
cd knowLib
cp .env.example .env
vim .env  # 填 DOMAIN 和 OPENAI_API_KEY

# 2. 一键 bootstrap(生成 secrets / TOTP / CouchDB 库 / 默认 prompt)
./scripts/bootstrap.sh

# 扫描终端打印的 QR 码到手机 Authenticator
# 抄好 E2E_PASSPHRASE 到密码管理器(丢失 = 数据无法解密)

# 3. 启动
docker compose up -d

# 4. 浏览器打开 https://<DOMAIN>,输 TOTP 6 位码登录

# 5. 在每个 Obsidian 端装 Self-hosted LiveSync 插件,填:
#    - URI:        https://<DOMAIN>/sync
#    - Username:   obsidian
#    - Password:   <COUCHDB_OBSIDIAN_PASSWORD>
#    - Database:   obsidian-vault
#    - E2E密钥:    <E2E_PASSPHRASE>
```

## 服务清单

| 服务 | 语言 | 作用 |
|---|---|---|
| `caddy` | — | TLS 终结 + 反向代理 |
| `couchdb` | — | LiveSync 后端 |
| `qdrant` | — | 向量数据库 |
| `embedder` | Python | bge-m3 嵌入 + fsnotify 索引 |
| `mirror` | Go | CouchDB ↔ 文件系统双向同步 |
| `tidy` | Go | AI 整理 worker(读 inbox / 写 notes) |
| `api` | Go | REST + MCP server + TOTP 登录 |
| `web` | TypeScript | Next.js + PWA(快捕/监控/设置) |

## 目录布局

```
vault/
├── inbox/        ← 你随手记进这里(任意端)
├── notes/        ← AI 整理后归位(按 category 树)
│   学习/高数/微分方程/...
│   随记/...
│   备忘/...
├── atlas/        ← AI 维护的 MOC(主题地图)
└── .knowlib/     ← 本地状态(不同步到客户端)
    ├── tidy.log
    └── prompts/
        └── tidy.md  ← 整理 prompt 模板,你随时可改
```

## AI 调用接口

任意 AI 客户端可通过下列方式访问你的知识库:

- **MCP**: `https://<DOMAIN>/mcp` (SSE + JSON-RPC),工具 `search_notes` / `get_note` / `list_notes` / `capture_note`
- **REST**: `https://<DOMAIN>/api/{search,note,list,capture,...}` (Bearer token)
- **OpenAI tool schema**: `packages/skill-bundle/openai-tools.json`
- **Claude Code skill**: `packages/skill-bundle/claude-skill/`

设置页可一键下载注入了你 server URL 与 token 的个人版 zip。

## 文档与计划

- 完整设计与决策:`C:\Users\42236\.claude\plans\rustling-napping-summit.md`
- Phase 1 范围:同步 + Web PWA + 手动 AI 整理 + Qdrant + REST/MCP/Skill + TOTP
- Phase 2:定时整理 cron + Telegram bot + Web 内置 RAG 对话
- Phase 3:飞书入口 + AI 润色(diff 预览) + 知识图谱可视化

## 安全

- TOTP-only 登录,JWT 24h 过期
- 长效 API token 用于 MCP / skill 客户端,可随时吊销
- `/api/auth/login` 速率限制
- LiveSync e2e 加密 — CouchDB 上看到的数据是密文
- CouchDB 不直接对外暴露,仅通过 Caddy `/sync` 反代

## 许可证

私有项目。
