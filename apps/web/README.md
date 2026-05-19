# apps/web — Next.js + PWA 前端

knowLib 的 Web 入口:快捕、Inbox 整理触发、笔记浏览、设置。

## 路由

- `/login` — TOTP 6 位输入
- `/capture` — **PWA 主页 / start_url**。极简一句话页,Ctrl/⌘+Enter 保存
- `/inbox` — Inbox 监控 + 整理触发(逐篇 / 全部)
- `/notes` — 笔记只读浏览(分类树 + Markdown 渲染);编辑请回 Obsidian
- `/settings` — LLM、Embedding、整理 prompt、API tokens、Skill 包下载、TOTP

## 技术栈

- Next.js 14 App Router · React 18 · TypeScript strict
- Tailwind v3 · 自定义 shadcn-style components(只内置用得到的)
- next-pwa(Service Worker + manifest)
- Dexie(IndexedDB 离线快捕队列)
- react-hook-form + zod(表单)
- react-markdown / remark-gfm / rehype-highlight(笔记渲染)

## 开发

```bash
pnpm install
pnpm dev
```

Web 通过 `app/api/*` 代理路由把所有 `/api/*` 请求转发到 `API_INTERNAL_URL`(默认 `http://api:8080`),并把
`klib_session` cookie 中的 JWT 转成 `Authorization: Bearer …`。

## 构建

```bash
pnpm build           # standalone 输出到 .next/standalone
node scripts/gen-icons.js   # 生成占位图标(已构建好的不用再跑)
```

Docker 镜像:`docker build -t knowlib-web ./apps/web`

## 离线 / PWA 行为

- `/capture` 在 `navigator.onLine === false` 或 fetch 失败时把内容写入 IndexedDB(`captures`)
- 联网后由 `online` 事件 + `focus` 事件触发 flush,逐条 POST 到 `/api/capture`
- Service Worker 不缓存 `/api/*`,只缓存静态资源 + 页面骨架,navigation 失败回落到 `/offline`

## 仍待延后到 Phase 2 的能力

- TOTP 重新绑定 UI(目前只能用 bootstrap.sh)
- 整理「定时 / 阈值」模式(UI 已禁用占位)
- Web 内置 RAG 对话(目前只在 `/notes` 浏览)
