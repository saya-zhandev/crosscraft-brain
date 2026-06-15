# syntax=docker/dockerfile:1

# ─────────────────────────────────────────────────────────────────────────────
# crosscraft-brain — a single Go binary that embeds the Vite SPA.
# Build context is the repo ROOT. Three stages:
#   web      pnpm workspace builds apps/web  -> server/web/dist
#   build    Go compiles the binary with the embedded SPA
#   runtime  distroless image with just the static binary
# ─────────────────────────────────────────────────────────────────────────────

# ── web: build the Vite SPA into server/web/dist ──
FROM node:20-bookworm-slim AS web
ENV PNPM_HOME=/pnpm
ENV PATH=$PNPM_HOME:$PATH
RUN corepack enable
WORKDIR /app
COPY . .
RUN pnpm install --frozen-lockfile
RUN pnpm --filter @crosscraft/web build

# ── build: compile the Go server (embeds server/web/dist) ──
FROM golang:1.26-bookworm AS build
WORKDIR /src/server
# Module graph first for layer caching.
COPY server/go.mod server/go.sum ./
RUN go mod download
# Server sources, plus the SPA built in the previous stage.
COPY server/ ./
COPY --from=web /app/server/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/crosscraft ./cmd/crosscraft

# ── runtime: minimal image with just the static binary ──
FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
ENV PORT=8080
COPY --from=build /out/crosscraft /app/crosscraft
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/crosscraft"]
