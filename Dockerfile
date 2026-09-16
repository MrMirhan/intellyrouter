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

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
 && addgroup -S intellyrouter && adduser -S -G intellyrouter intellyrouter
COPY --from=go /out/intellyrouter /usr/local/bin/intellyrouter
COPY --from=go /out/intelly-eval    /usr/local/bin/intelly-eval
ENV INTELLYROUTER_ADDR=0.0.0.0:7117 \
    INTELLYROUTER_DATA=/data \
    INTELLYROUTER_EVAL_TASKS=/eval-tasks
VOLUME ["/data", "/eval-tasks"]
EXPOSE 7117
USER intellyrouter
# Exec form with a shell is the simplest portable way to expand env vars into
# flag values without writing a wrapper entrypoint script.
ENTRYPOINT ["sh", "-c", "exec intellyrouter -addr \"$INTELLYROUTER_ADDR\" -data \"$INTELLYROUTER_DATA\" -eval-tasks \"$INTELLYROUTER_EVAL_TASKS\""]
