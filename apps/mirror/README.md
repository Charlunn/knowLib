# apps/mirror — vault-mirror (Go)

CouchDB ↔ 本地 vault 文件系统双向同步。Self-hosted LiveSync 兼容(分块格式 + e2e 加密)。

## 待实现

- `cmd/mirror/main.go`
- `internal/livesync/` — LiveSync 文档分块、组装、e2e 加密(参考 obsidian-livesync 的 livesync-commonlib)
- `internal/couchdb/` — CouchDB 客户端 + _changes feed
- `internal/fs/` — fsnotify 监听
- `internal/state/` — BoltDB,记录 path → rev/mtime
- `Dockerfile`
- `go.mod` / `go.sum`

## 风险

LiveSync 分块格式实现是最大不确定项。先用 Go 试,搞不定切到 Node。
