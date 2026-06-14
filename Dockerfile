# syntax=docker/dockerfile:1

# ─────────────────────────────────────────────────────────────────────────────
# crosscraft-brain studio — monorepo (pnpm) → Next.js standalone server.
# Build context is the repo ROOT. Two targets:
#   builder  full workspace + tooling (also used by the one-shot `migrate` service)
#   runner   slim production image that serves the studio
# ─────────────────────────────────────────────────────────────────────────────

FROM node:20-bookworm-slim AS base
ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH
RUN corepack enable
WORKDIR /app

# ── builder: install all workspaces and build the studio ──
FROM base AS builder
# Copy the whole monorepo (see .dockerignore for what's excluded).
COPY . .
RUN pnpm install --frozen-lockfile
RUN pnpm --filter @crosscraft/studio build

# ── runner: minimal image running the standalone server ──
FROM node:20-bookworm-slim AS runner
WORKDIR /app
ENV NODE_ENV=production
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

# Run as the unprivileged node user shipped with the base image.
USER node

# Standalone output already contains a pruned node_modules + traced workspace packages.
COPY --from=builder --chown=node:node /app/apps/studio/.next/standalone ./
COPY --from=builder --chown=node:node /app/apps/studio/.next/static ./apps/studio/.next/static

EXPOSE 3000
CMD ["node", "apps/studio/server.js"]
