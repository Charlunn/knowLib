#!/usr/bin/env bash
# 启动 knowLib standalone 模式（Caddy 直接占用 80/443，自动 HTTPS）
# 使用场景: 服务器上没有其他服务占用 80/443
# 使用方式: ./scripts/start-standalone.sh [up|down|restart|build|logs]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

# 兼容 docker compose (v2 插件) 和 docker-compose (v1 独立二进制)
if docker compose version >/dev/null 2>&1; then
  DC="docker compose"
elif command -v docker-compose >/dev/null 2>&1; then
  DC="docker-compose"
else
  echo "ERROR: neither 'docker compose' nor 'docker-compose' found" >&2
  exit 1
fi

CMD="${1:-up}"
COMPOSE="$DC -f docker-compose.yml"

case "$CMD" in
  up)
    echo "==> Starting knowLib (standalone mode)"
    $COMPOSE up -d
    echo ""
    echo "Services:"
    $COMPOSE ps
    echo ""
    echo "Access: https://$(grep '^DOMAIN=' .env | cut -d= -f2)"
    ;;
  down)
    echo "==> Stopping knowLib (standalone)"
    $COMPOSE down
    ;;
  restart)
    echo "==> Restarting knowLib (standalone)"
    $COMPOSE down
    $COMPOSE up -d
    ;;
  build)
    echo "==> Rebuilding knowLib (standalone)"
    $COMPOSE up -d --build
    ;;
  logs)
    shift || true
    $COMPOSE logs -f "$@"
    ;;
  *)
    echo "Usage: $0 [up|down|restart|build|logs [service]]"
    exit 1
    ;;
esac
