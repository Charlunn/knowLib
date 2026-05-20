#!/usr/bin/env bash
# resolve-conflicts.sh — 自动合并 Remotely Save 产生的冲突副本
#
# Remotely Save 冲突文件命名模式:
#   原文件: notes/学习/微积分.md
#   冲突:   notes/学习/微积分 (conflicted copy YYYY-MM-DD).md
#
# 合并策略（简单且安全）:
#   1. 如果原文件不存在 → 直接重命名冲突文件为原文件
#   2. 如果原文件存在且内容相同 → 删除冲突副本
#   3. 如果原文件存在且内容不同 → 取更长的版本（更多内容 = 更完整）
#      并把被丢弃的版本备份到 .knowlib/conflict-backups/
#
# 用法:
#   ./scripts/resolve-conflicts.sh           # 扫描并合并
#   ./scripts/resolve-conflicts.sh --dry-run # 只报告，不动文件
#
# 可以加到 crontab 每小时跑一次:
#   0 * * * * /root/knowLib/scripts/resolve-conflicts.sh >> /root/knowLib/data/vault/.knowlib/conflict-resolve.log 2>&1

set -euo pipefail

VAULT="${VAULT_PATH:-/root/knowLib/data/vault}"
BACKUP_DIR="$VAULT/.knowlib/conflict-backups"
DRY_RUN="${1:-}"
RESOLVED=0
SKIPPED=0

mkdir -p "$BACKUP_DIR"

log() { echo "$(date -u +%Y-%m-%dT%H:%M:%SZ) $*"; }

# Find all conflict files. Remotely Save uses this pattern:
#   "filename (conflicted copy YYYY-MM-DD).ext"
# Some versions also use:
#   "filename.conflict.ext" or "filename.sync-conflict-YYYYMMDD.ext"
find "$VAULT" -type f \( \
  -name "* (conflicted copy *).md" -o \
  -name "*.conflict.md" -o \
  -name "*.sync-conflict-*.md" \
\) | while read -r conflict_file; do
  # Derive the original filename by stripping the conflict suffix
  original=""
  if [[ "$conflict_file" =~ (.+)\ \(conflicted\ copy\ [0-9-]+\)\.md$ ]]; then
    original="${BASH_REMATCH[1]}.md"
  elif [[ "$conflict_file" =~ (.+)\.conflict\.md$ ]]; then
    original="${BASH_REMATCH[1]}.md"
  elif [[ "$conflict_file" =~ (.+)\.sync-conflict-[0-9]+\.md$ ]]; then
    original="${BASH_REMATCH[1]}.md"
  else
    log "SKIP: unrecognized conflict pattern: $conflict_file"
    SKIPPED=$((SKIPPED + 1))
    continue
  fi

  if [[ ! -f "$original" ]]; then
    # Case 1: original doesn't exist, just rename
    log "RENAME: $conflict_file → $original"
    if [[ "$DRY_RUN" != "--dry-run" ]]; then
      mv "$conflict_file" "$original"
    fi
    RESOLVED=$((RESOLVED + 1))
    continue
  fi

  # Compare content
  if cmp -s "$original" "$conflict_file"; then
    # Case 2: identical content, delete conflict
    log "DELETE (identical): $conflict_file"
    if [[ "$DRY_RUN" != "--dry-run" ]]; then
      rm "$conflict_file"
    fi
    RESOLVED=$((RESOLVED + 1))
    continue
  fi

  # Case 3: different content, keep the longer one
  orig_size=$(wc -c < "$original")
  conf_size=$(wc -c < "$conflict_file")

  if [[ $conf_size -gt $orig_size ]]; then
    # Conflict version is more complete, use it
    log "MERGE (conflict longer): $conflict_file → $original (backup original)"
    if [[ "$DRY_RUN" != "--dry-run" ]]; then
      backup_name="$BACKUP_DIR/$(basename "$original").$(date +%s).bak"
      cp "$original" "$backup_name"
      mv "$conflict_file" "$original"
    fi
  else
    # Original is more complete (or same size), keep original
    log "MERGE (original longer): keep $original (backup conflict)"
    if [[ "$DRY_RUN" != "--dry-run" ]]; then
      backup_name="$BACKUP_DIR/$(basename "$conflict_file").$(date +%s).bak"
      mv "$conflict_file" "$backup_name"
    fi
  fi
  RESOLVED=$((RESOLVED + 1))
done

log "Done. Resolved: $RESOLVED, Skipped: $SKIPPED"
