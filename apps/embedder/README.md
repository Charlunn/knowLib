# apps/embedder — embedding 索引服务 (Python)

`fsnotify` 监听 vault → 切块 → 嵌入 → 写 Qdrant。同时提供 OpenAI 兼容 `/v1/embeddings` 给其他服务做 query embedding。

## 模型

默认 `BAAI/bge-m3` (1024 维,中英文 SOTA)。模型缓存到 `/cache`(挂载 `./data/embedder-cache`)。

可在 Web 设置页通过 `EMBED_MODEL` 切换。

## 待实现

- `main.py` — FastAPI app + watcher 协程
- `chunker.py` — H2/H3 切块,800 token 上限,80 重叠
- `vault_index.py` — 全量索引、增量索引、删除
- `qdrant_client.py` — 封装 collection 创建、upsert、search
- `embed.py` — 模型加载,`/v1/embeddings` 路由
- `requirements.txt`
- `Dockerfile`
