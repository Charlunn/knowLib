#!/usr/bin/env bash
# knowLib bootstrap
# 用法:cp .env.example .env && 编辑 DOMAIN 与 OPENAI_API_KEY,然后:
#   ./scripts/bootstrap.sh
#
# 做的事:
#  1. 检查 .env 必填项
#  2. 生成 random secrets(JWT_SECRET / E2E_PASSPHRASE / COUCHDB 密码)并写回 .env(只覆盖未设置的字段)
#  3. 生成 TOTP secret 并打印 QR 码(扫到 Authenticator)
#  4. 生成长效 API token 写到 data/api-state/initial-token.txt
#  5. 把 prompt 模板拷到 data/vault/.knowlib/prompts/
#  6. 创建 vault 子目录骨架
#  7. 启动 docker compose,等 CouchDB 就绪后创建 obsidian-vault 库 + obsidian 普通账号
#
# 重新跑安全:已写入的 secrets 不会被覆盖;TOTP secret 不会重新生成。

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

ENV_FILE="$ROOT/.env"
if [[ ! -f "$ENV_FILE" ]]; then
  echo "missing .env — run 'cp .env.example .env' and fill DOMAIN + OPENAI_API_KEY first" >&2
  exit 1
fi

# shellcheck disable=SC1090
source "$ENV_FILE"

if [[ -z "${DOMAIN:-}" || "$DOMAIN" == "knowlib.example.com" ]]; then
  echo "set DOMAIN in .env to your real domain" >&2
  exit 1
fi
if [[ -z "${OPENAI_API_KEY:-}" || "$OPENAI_API_KEY" == "sk-your-deepseek-key" ]]; then
  echo "set OPENAI_API_KEY in .env" >&2
  exit 1
fi

rand() { openssl rand -base64 "$1" | tr -d '\n=+/' | cut -c1-"$1"; }
rand_hex() { openssl rand -hex "$1"; }

set_env() {
  # set_env KEY VALUE — only writes if KEY is empty in .env
  local key="$1" val="$2"
  local cur
  cur="$(grep -E "^${key}=" "$ENV_FILE" | head -n1 | cut -d= -f2- || true)"
  if [[ -z "$cur" || "$cur" == "change-me-strong-random" || "$cur" == "change-me-very-long-random" || "$cur" == "change-me-random-64-bytes" ]]; then
    if grep -qE "^${key}=" "$ENV_FILE"; then
      # GNU/BSD-portable in-place: write to tmp file
      awk -v k="$key" -v v="$val" 'BEGIN{FS=OFS="="} $1==k {print k"="v; next} {print}' "$ENV_FILE" > "$ENV_FILE.tmp"
      mv "$ENV_FILE.tmp" "$ENV_FILE"
    else
      printf '\n%s=%s\n' "$key" "$val" >> "$ENV_FILE"
    fi
    echo "  generated $key"
  else
    echo "  kept $key (already set)"
  fi
}

echo "==> Generating secrets"
set_env COUCHDB_PASSWORD "$(rand 32)"
set_env COUCHDB_OBSIDIAN_PASSWORD "$(rand 32)"
set_env E2E_PASSPHRASE "$(rand 48)"
set_env JWT_SECRET "$(rand_hex 32)"

echo
echo "==> TOTP enrollment"
if grep -qE '^TOTP_SECRET=.+' "$ENV_FILE"; then
  echo "  TOTP_SECRET already set, skipping enrollment (delete the line in .env to regenerate)"
else
  TOTP_SECRET="$(openssl rand 20 | base32 | tr -d '=')"
  awk -v v="$TOTP_SECRET" 'BEGIN{FS=OFS="="} $1=="TOTP_SECRET"{print "TOTP_SECRET="v; next} {print}' "$ENV_FILE" > "$ENV_FILE.tmp"
  mv "$ENV_FILE.tmp" "$ENV_FILE"
  OTP_URI="otpauth://totp/${TOTP_ISSUER:-knowLib}:${TOTP_ACCOUNT:-me}?secret=${TOTP_SECRET}&issuer=${TOTP_ISSUER:-knowLib}"
  echo "  TOTP secret: $TOTP_SECRET"
  echo "  Add to Authenticator (URI): $OTP_URI"
  if command -v qrencode >/dev/null 2>&1; then
    qrencode -t ANSIUTF8 "$OTP_URI"
  else
    echo "  (install 'qrencode' to render QR in terminal; otherwise paste the URI into a QR generator)"
  fi
fi

echo
echo "==> Reminding you to back up E2E_PASSPHRASE"
E2E="$(grep -E '^E2E_PASSPHRASE=' "$ENV_FILE" | cut -d= -f2-)"
echo "  >>> COPY THIS TO YOUR PASSWORD MANAGER NOW <<<"
echo "  E2E_PASSPHRASE = $E2E"
echo "  This is needed to set up Obsidian LiveSync on every device, AND for vault-mirror"
echo "  to decrypt your data. Lose it and your encrypted history is unrecoverable."
echo

echo "==> Creating vault skeleton"
mkdir -p data/vault/inbox data/vault/notes data/vault/atlas data/vault/.knowlib/prompts
mkdir -p data/api-state data/mirror-state data/embedder-cache data/caddy/log

if [[ ! -f data/vault/.knowlib/prompts/tidy.md ]]; then
  cp scripts/templates/prompts/tidy.md data/vault/.knowlib/prompts/tidy.md
  echo "  installed default tidy prompt"
fi

# Write a placeholder initial-token file that will be populated after first api startup.
# The api creates real tokens via /api/tokens; this file is just a reminder.
if [[ ! -f data/api-state/initial-token.txt ]]; then
  cat > data/api-state/initial-token.txt <<'TOKENEOF'
# Initial API token will be available after first login via the web UI.
# Go to https://<DOMAIN>/settings > API Tokens > Create Token
# Alternatively use the REST API after logging in with your TOTP code.
TOKENEOF
  echo "  created data/api-state/initial-token.txt (placeholder)"
fi

# ensure ownership for non-root container users (best-effort, ignore on Windows)
chmod -R 755 data/vault 2>/dev/null || true

# ── 选择启动模式 ──────────────────────────────────────────────────────────────
# 默认 standalone（Caddy 直接占 80/443）。
# 如果 80 端口被占，自动切换到 behind-nginx 模式（Caddy 绑 127.0.0.1:8080）。
echo
if ss -tlnp 2>/dev/null | grep -q ':80 \|:80$' || lsof -i:80 >/dev/null 2>&1; then
  MODE="behind-nginx"
  COMPOSE_FILE="docker-compose.behind-nginx.yml"
  echo "==> Port 80 is in use — starting in behind-nginx mode"
  echo "    Caddy will listen on 127.0.0.1:8080 only."
  echo "    See scripts/nginx/knowlib.conf for the nginx server block template."
else
  MODE="standalone"
  COMPOSE_FILE="docker-compose.yml"
  echo "==> Starting in standalone mode (Caddy handles 80/443 + auto HTTPS)"
fi

echo "==> Starting docker compose (this may take a while on first run)"
$DC -f "$COMPOSE_FILE" up -d

echo
echo "==> Waiting for CouchDB..."
for i in {1..60}; do
  if curl -sf "http://localhost:5984" >/dev/null 2>&1 || \
     $DC -f "$COMPOSE_FILE" exec -T couchdb curl -sf http://localhost:5984 >/dev/null 2>&1; then
    break
  fi
  sleep 2
done

echo "==> Configuring CouchDB"
COUCH_AUTH="${COUCHDB_USER}:$(grep -E '^COUCHDB_PASSWORD=' "$ENV_FILE" | cut -d= -f2-)"
COUCH_URL_INTERNAL="http://${COUCH_AUTH}@couchdb:5984"

# create system dbs (idempotent), main db, obsidian user
$DC -f "$COMPOSE_FILE" exec -T couchdb bash -lc "
  set -e
  for db in _users _replicator _global_changes ${COUCHDB_DB:-obsidian-vault}; do
    curl -sf -X PUT '${COUCH_URL_INTERNAL}/'\$db || true
  done

  # create / update non-admin obsidian user
  OB_PASS='$(grep -E '^COUCHDB_OBSIDIAN_PASSWORD=' "$ENV_FILE" | cut -d= -f2-)'
  curl -sf -X PUT '${COUCH_URL_INTERNAL}/_users/org.couchdb.user:${COUCHDB_OBSIDIAN_USER:-obsidian}' \
    -H 'Content-Type: application/json' \
    -d '{\"name\":\"${COUCHDB_OBSIDIAN_USER:-obsidian}\",\"password\":\"'\"\$OB_PASS\"'\",\"roles\":[],\"type\":\"user\"}' || true

  # CORS for Obsidian
  curl -sf -X PUT '${COUCH_URL_INTERNAL}/_node/_local/_config/httpd/enable_cors' -d '\"true\"'
  curl -sf -X PUT '${COUCH_URL_INTERNAL}/_node/_local/_config/cors/credentials' -d '\"true\"'
  curl -sf -X PUT '${COUCH_URL_INTERNAL}/_node/_local/_config/cors/origins' -d '\"app://obsidian.md,capacitor://localhost,http://localhost\"'
  curl -sf -X PUT '${COUCH_URL_INTERNAL}/_node/_local/_config/cors/methods' -d '\"GET,PUT,POST,HEAD,DELETE\"'
  curl -sf -X PUT '${COUCH_URL_INTERNAL}/_node/_local/_config/cors/headers' -d '\"accept,authorization,content-type,origin,referer,x-csrf-token\"'

  # raise per-doc revision limit a bit (LiveSync friendly)
  curl -sf -X PUT '${COUCH_URL_INTERNAL}/_node/_local/_config/couchdb/max_document_size' -d '\"50000000\"'
  curl -sf -X PUT '${COUCH_URL_INTERNAL}/_node/_local/_config/chttpd/max_http_request_size' -d '\"4294967296\"'
"

echo
echo "==> Bootstrap complete (mode: $MODE)"
echo
if [[ "$MODE" == "standalone" ]]; then
  echo "Next steps:"
  echo "  1. Open https://${DOMAIN}/login and enter your TOTP code."
  echo "  2. On every Obsidian device install 'Self-hosted LiveSync' plugin and configure:"
  echo "       URI:        https://${DOMAIN}/sync"
  echo "       Username:   ${COUCHDB_OBSIDIAN_USER:-obsidian}"
  echo "       Password:   $(grep -E '^COUCHDB_OBSIDIAN_PASSWORD=' "$ENV_FILE" | cut -d= -f2-)"
  echo "       Database:   ${COUCHDB_DB:-obsidian-vault}"
  echo "       E2E pass:   <copy from your password manager>"
  echo "  3. Visit /settings on the web app to download your personal skill bundle."
else
  echo "Next steps (behind-nginx mode):"
  echo "  1. Install the nginx server block:"
  echo "       sudo cp scripts/nginx/knowlib.conf /etc/nginx/conf.d/knowlib.conf"
  echo "       sudo sed -i 's/YOUR_DOMAIN/${DOMAIN}/g' /etc/nginx/conf.d/knowlib.conf"
  echo "       sudo nginx -t && sudo systemctl reload nginx"
  echo "  2. Sign TLS certificate:"
  echo "       sudo certbot --nginx -d ${DOMAIN}"
  echo "  3. Open https://${DOMAIN}/login and enter your TOTP code."
  echo "  4. On every Obsidian device install 'Self-hosted LiveSync' plugin and configure:"
  echo "       URI:        https://${DOMAIN}/sync"
  echo "       Username:   ${COUCHDB_OBSIDIAN_USER:-obsidian}"
  echo "       Password:   $(grep -E '^COUCHDB_OBSIDIAN_PASSWORD=' "$ENV_FILE" | cut -d= -f2-)"
  echo "       Database:   ${COUCHDB_DB:-obsidian-vault}"
  echo "       E2E pass:   <copy from your password manager>"
  echo "  5. Visit /settings on the web app to download your personal skill bundle."
fi
