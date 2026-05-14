# syntax=docker/dockerfile:1

# Frontend build
FROM oven/bun:1-alpine AS frontend-builder

WORKDIR /app/frontend

COPY frontend/package.json frontend/bun.lock ./
RUN bun install --frozen-lockfile

COPY frontend/ .
RUN bun run build

# Backend build
FROM golang:1.22-alpine AS backend-builder

WORKDIR /src

COPY backend/go.mod backend/go.sum ./
RUN go mod download

COPY backend/ .
RUN CGO_ENABLED=0 GOOS=linux GIN_MODE=release go build -trimpath -ldflags="-s -w" -o /out/omnirelay ./cmd/server

# Production image
FROM caddy:2-alpine

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S omnirelay \
    && adduser -S -G omnirelay omnirelay \
    && mkdir -p /app/data \
    && chown -R omnirelay:omnirelay /app

WORKDIR /app

COPY --from=backend-builder /out/omnirelay /app/omnirelay
COPY --from=frontend-builder /app/frontend/dist /usr/share/caddy
COPY frontend/Caddyfile /etc/caddy/Caddyfile

ENV LISTEN_ADDR=:8080
ENV DATABASE_PATH=/app/data/omnirelay.db

EXPOSE 80

USER omnirelay

ENTRYPOINT ["/bin/sh", "-c", "/app/omnirelay & caddy run --config /etc/caddy/Caddyfile --adapter caddyfile"]
