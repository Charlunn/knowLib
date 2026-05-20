#!/usr/bin/env bash
# 启动 knowLib standalone 模式（Caddy 直接占用 80/443，自动 HTTPS）
# 使用场景: 服务器上没有其他服务占用 80/443
# 使用方式: ./scripts/start-standalone.sh [up|down|restart|logs]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

CMD="${1:-up}"
COMPOSE="docker compose -f docker-compose.yml"

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
