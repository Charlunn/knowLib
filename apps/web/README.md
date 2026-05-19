# apps/web — Next.js + PWA 前端

Web 监控、快捕、整理触发、设置。

## 路由

- `/login` — TOTP 6 位输入框
- `/capture` — 极简一句话页(快捕,PWA 主页)
- `/inbox` — inbox 监控 + 整理触发
- `/notes` — 浏览(只读;编辑去 Obsidian)
- `/settings` — LLM provider、整理 prompt、API tokens、TOTP 重绑

## PWA

- `next-pwa` Service Worker
- `public/manifest.json`
- 离线写入用 IndexedDB 队列,联网后 flush 到 `/api/capture`
- 加到主屏后体验接近原生

## 待实现

- Next.js 14 App Router 骨架
- Tailwind + shadcn/ui
- Service Worker + manifest
- IndexedDB 队列(Dexie)
- 与 API 通信封装(JWT cookie)
- `Dockerfile` (多阶段 + standalone output)
