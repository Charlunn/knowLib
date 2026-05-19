# syntax=docker/dockerfile:1.7
FROM node:20-alpine AS deps
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@9.12.3 --activate
COPY package.json ./
COPY pnpm-lock.yaml* ./
RUN if [ -f pnpm-lock.yaml ]; then pnpm install --frozen-lockfile; else pnpm install --no-frozen-lockfile; fi

FROM node:20-alpine AS build
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@9.12.3 --activate
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN pnpm build

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
RUN addgroup -S mirror && adduser -S mirror -G mirror
COPY --from=build --chown=mirror:mirror /app/node_modules ./node_modules
COPY --from=build --chown=mirror:mirror /app/dist ./dist
COPY --from=build --chown=mirror:mirror /app/package.json ./package.json
USER mirror
CMD ["node", "dist/main.js"]
