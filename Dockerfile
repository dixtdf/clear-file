# ---------- stage 1: build the Vue 3 web UI ----------
FROM node:22-alpine AS web

WORKDIR /src/web
COPY web/package.json web/package-lock.json* ./
RUN if [ -f package-lock.json ]; then npm ci; else npm install; fi

COPY web/ ./
RUN npm run build

# ---------- stage 2: build the Go binary ----------
FROM golang:1.23-alpine AS build

# 发版时由 CI 注入：docker build --build-arg VERSION=0.1.0 --build-arg COMMIT=abc1234 .
ARG VERSION=dev
ARG COMMIT=none

WORKDIR /src
ENV CGO_ENABLED=0 GOOS=linux

COPY go.mod ./
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/*.go ./web/
COPY --from=web /src/web/dist ./web/dist

RUN go build -trimpath \
    -ldflags "-s -w \
      -X file-cleaner/internal/version.Version=${VERSION} \
      -X file-cleaner/internal/version.Commit=${COMMIT}" \
    -o /out/file-cleaner ./cmd/server

# ---------- stage 3: minimal runtime ----------
FROM alpine:3.20

ARG VERSION=dev
LABEL org.opencontainers.image.title="file-cleaner" \
      org.opencontainers.image.description="clear-file: web file cleaner + disk analyzer (embedded web UI)" \
      org.opencontainers.image.version="${VERSION}"

RUN apk add --no-cache ca-certificates tzdata && \
    adduser -D -u 10001 cleaner

ENV FILE_CLEANER_ROOT=/ \
    FILE_CLEANER_ADDR=:6888 \
    TZ=Asia/Shanghai

COPY --from=build /out/file-cleaner /usr/local/bin/file-cleaner

EXPOSE 6888
# Run as root because NAS mounts are often root-owned; remove the
# USER line or map ownership if your mount is writable by uid 10001.
# USER cleaner

ENTRYPOINT ["/usr/local/bin/file-cleaner"]
