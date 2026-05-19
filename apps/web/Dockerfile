# syntax=docker/dockerfile:1.7

# ── Stage 1: deps ───────────────────────────────────────────────
FROM node:20-alpine AS deps
WORKDIR /app

# pnpm via corepack
RUN corepack enable && corepack prepare pnpm@9.12.3 --activate

COPY package.json ./
# Lockfile may not exist in initial commits; fall back to fresh install if missing.
COPY pnpm-lock.yaml* ./
RUN if [ -f pnpm-lock.yaml ]; then \
      pnpm install --frozen-lockfile; \
    else \
      pnpm install --no-frozen-lockfile; \
    fi

# ── Stage 2: builder ────────────────────────────────────────────
FROM node:20-alpine AS builder
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@9.12.3 --activate
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
RUN pnpm build

# ── Stage 3: runner ─────────────────────────────────────────────
FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

RUN addgroup -S nextjs && adduser -S nextjs -G nextjs

# next.config.js sets `output: 'standalone'` — copy the slim server bundle.
COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nextjs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nextjs /app/.next/static ./.next/static

USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
