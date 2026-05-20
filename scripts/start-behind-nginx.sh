#!/usr/bin/env bash
# 启动 knowLib behind-nginx 模式（Caddy 仅绑 127.0.0.1:8080，TLS 由宿主 nginx 处理）
# 使用场景: 服务器上已有 nginx 占用 80/443
#
# 首次使用前请先：
#   1. 把 scripts/nginx/knowlib.conf 复制到 /etc/nginx/conf.d/knowlib.conf
#      并把其中的 YOUR_DOMAIN 改成你的域名
#   2. 用 certbot 签证书:
#      sudo certbot --nginx -d your.domain.com
#   3. 运行本脚本
#
# 使用方式: ./scripts/start-behind-nginx.sh [up|down|restart|build|logs]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

CMD="${1:-up}"
COMPOSE="docker compose -f docker-compose.behind-nginx.yml"

case "$CMD" in
  up)
    echo "==> Starting knowLib (behind-nginx mode)"
    $COMPOSE up -d
    echo ""
    echo "Services:"
    $COMPOSE ps
    echo ""
    DOMAIN="$(grep '^DOMAIN=' .env | cut -d= -f2)"
    echo "Caddy listening on: 127.0.0.1:8080"
    echo "Access via nginx:   https://${DOMAIN}"
    echo ""
    echo "If nginx is not yet configured, see: scripts/nginx/knowlib.conf"
    ;;
  down)
    echo "==> Stopping knowLib (behind-nginx)"
    $COMPOSE down
    ;;
  restart)
    echo "==> Restarting knowLib (behind-nginx)"
    $COMPOSE down
    $COMPOSE up -d
    ;;
  build)
    echo "==> Rebuilding knowLib (behind-nginx)"
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
