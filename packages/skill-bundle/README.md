# skill-bundle

可分发的 AI 客户端配置包,让任何支持 MCP / OpenAI tool / Claude skill 的客户端用一份配置就能访问你的 knowLib。

设置页有「下载我的 skill 包」按钮,自动注入你的 server URL 和一份新 token,打 zip 给你。

## 内容

- `mcp.json` — MCP 配置片段(Claude Desktop / Cursor 等)
- `openai-tools.json` — OpenAI tool schema(任何 OpenAI 兼容客户端)
- `claude-skill/SKILL.md` + 调用脚本 — Claude Code skill
- `INSTALL.md` — 三种用法的 30 秒上手

## 实现说明

模板文件用 `{{SERVER_URL}}` 和 `{{API_TOKEN}}` 占位符。运行时 api 服务在打包 zip 前把模板里的占位符替换成真实值，每次下载会生成新的长效 token。
