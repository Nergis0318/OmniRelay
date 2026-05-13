# syntax=docker/dockerfile:1

FROM golang:1.22-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/omnirelay ./cmd/server

FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S omnirelay \
    && adduser -S -G omnirelay omnirelay \
    && mkdir -p /app/data \
    && chown -R omnirelay:omnirelay /app

WORKDIR /app

COPY --from=builder /out/omnirelay /app/omnirelay

ENV LISTEN_ADDR=:8080
ENV DATABASE_PATH=/app/data/omnirelay.db

EXPOSE 8080

USER omnirelay

ENTRYPOINT ["/app/omnirelay"]
