# syntax=docker/dockerfile:1.7
# Mirror is plain npm — there's a package-lock.json checked in.
# Multi-stage so the runtime image stays small (no devDeps, no source).

FROM node:20-alpine AS deps
WORKDIR /app
# Use npm ci against the lockfile for reproducible installs.
COPY package.json package-lock.json ./
RUN npm ci

FROM node:20-alpine AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
RUN npx tsc -p .

FROM node:20-alpine AS runner
WORKDIR /app
ENV NODE_ENV=production
RUN addgroup -S mirror && adduser -S mirror -G mirror
# Production-only install (drops typescript/tsx and other devDeps).
COPY package.json package-lock.json ./
RUN npm ci --omit=dev && npm cache clean --force
COPY --from=build --chown=mirror:mirror /app/dist ./dist
USER mirror
CMD ["node", "dist/main.js"]
