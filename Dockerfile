# syntax=docker/dockerfile:1.7

# Build the web dashboard first so its dist/ is ready for the Go embed step.
# We pin to a slim alpine that ships bun, so contributors don't need a host
# bun install. pnpm is not used by the gateway; bun builds web/ directly.
FROM oven/bun:1-alpine AS web
WORKDIR /src/web
COPY web/package.json web/bun.lock* ./
RUN bun install --frozen-lockfile
COPY web/ ./
RUN bunx tsc --noEmit -p tsconfig.app.json \
 && bun run lint \
 && bun run build

FROM golang:1.27-alpine AS go
WORKDIR /src
RUN apk add --no-cache git ca-certificates
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -tags webui -ldflags="-s -w" \
    -o /out/intellyrouter ./cmd/intellyrouter \
 && CGO_ENABLED=0 go build -trimpath -tags webui -ldflags="-s -w" \
    -o /out/intelly-eval ./cmd/intelly-eval

FROM alpine:3.24
# Claude Code lives in the same image so subscription directors, advisors,
# and the eval runner can shell out to `claude`. The native installer pulls
# a musl-compatible binary, so we do not need npm or Node on this stage.
ARG CLAUDE_VERSION=stable
ENV USE_BUILTIN_RIPGREP=0
# The installer puts a launcher at $HOME/.local/bin/claude that symlinks into
# $HOME/.local/share. Install with HOME already pointing at the runtime user's
# home, because copying the tree afterwards leaves that symlink aimed at
# /root, which mode 0700 keeps the runtime user out of.
RUN apk add --no-cache ca-certificates tzdata bash curl libgcc libstdc++ ripgrep \
 && apk add --no-cache go python3 py3-pip \
 && addgroup -S intellyrouter && adduser -S -G intellyrouter -h /home/intellyrouter intellyrouter \
 && curl -fsSL https://claude.ai/install.sh -o /tmp/install.sh \
 && HOME=/home/intellyrouter bash /tmp/install.sh ${CLAUDE_VERSION} \
 && rm /tmp/install.sh \
 && chown -R intellyrouter:intellyrouter /home/intellyrouter
COPY --from=go /out/intellyrouter /usr/local/bin/intellyrouter
COPY --from=go /out/intelly-eval    /usr/local/bin/intelly-eval
ENV INTELLYROUTER_ADDR=0.0.0.0:7117 \
    INTELLYROUTER_DATA=/data \
    INTELLYROUTER_EVAL_TASKS=/eval-tasks \
    CLAUDE_CONFIG_DIR=/data/.claude \
    PATH=/home/intellyrouter/.local/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
VOLUME ["/data", "/eval-tasks"]
EXPOSE 7117
WORKDIR /home/intellyrouter
# The entrypoint stays root only long enough to take the data volume, which
# Dokploy and Coolify create owned by root, and then runs the gateway as
# intellyrouter. master.key is mode 0600, so the gateway cannot read its own
# key unless it owns the directory.
ENTRYPOINT ["sh", "-c", "mkdir -p \"$INTELLYROUTER_DATA\" \"$CLAUDE_CONFIG_DIR\" && chown -R intellyrouter:intellyrouter \"$INTELLYROUTER_DATA\" && exec su -s /bin/sh intellyrouter -c 'exec intellyrouter -addr \"$INTELLYROUTER_ADDR\" -data \"$INTELLYROUTER_DATA\" -eval-tasks \"$INTELLYROUTER_EVAL_TASKS\"'"]
