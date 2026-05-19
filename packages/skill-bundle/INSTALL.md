# 安装 knowLib skill 包

这份包让任意支持 MCP / OpenAI tool / Claude Code skill 的 AI 客户端,通过统一接口访问你自己的 knowLib 知识库。

如果你是从 knowLib 的 `/settings` 页面下载的 zip,所有 `{{SERVER_URL}}` / `{{API_TOKEN}}` 占位符已被替换成你的真实值。如果是手动复制,自己改一下。

## 三种用法

### 1. MCP 客户端 (Claude Desktop / Cursor / Cherry Studio)

把 `mcp.json` 里的 `mcpServers.knowlib` 块,合并到你 MCP 客户端的配置文件里。

**Claude Desktop**:`~/Library/Application Support/Claude/claude_desktop_config.json`(macOS) 或 `%APPDATA%\Claude\claude_desktop_config.json`(Windows)。

**Cursor**:`~/.cursor/mcp.json`。

合并后重启客户端。Claude / Cursor 应该能看到 4 个新工具:`knowlib_search_notes`、`knowlib_get_note`、`knowlib_list_notes`、`knowlib_capture_note`。

### 2. 任意 OpenAI 兼容客户端

`openai-tools.json` 里是 4 个 tool definition。把它们注入到你客户端的 `tools` 字段(OpenAI Chat Completions API)。

每个 tool 的 `_endpoint` 字段告诉你被调用时该打哪个 URL,`_auth` 是请求头。如果你的客户端支持声明式 tool calling(如 Coze、Dify、n8n),把 endpoint 配进对应 HTTP 节点即可。

### 3. Claude Code skill

复制 `claude-skill/` 整个文件夹到 `~/.claude/skills/knowlib/`(或者你 Claude Code 的项目 skills 目录)。

```bash
cp -r claude-skill ~/.claude/skills/knowlib
```

然后在 Claude Code 里输入 `/knowlib` 或者直接让 Claude「搜索我知识库里关于 X 的内容」,它会调用 `bin/knowlib.js` 与你的 knowLib 通信。

需要 Node 18+。脚本零外部依赖,只用内置 `fetch`。

## 验证安装

`claude-skill/` 下:

```bash
cd claude-skill
node bin/knowlib.js search "test"
```

如果返回 JSON(可能是空数组,如果还没记录),就成功了。如果报 401,token 被吊销了——回 `/settings` 重新下包。

## 安全

- API token 是长效凭证,等同密码。不要提交到 Git,不要发到聊天里。
- 在 `/settings` 页面可随时吊销旧 token,生成新的。
