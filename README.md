# IntellyRouter

**A local-first gateway that puts cheap models in front of Claude Code and
keeps the Claude subscription for the calls that matter.**

[![CI](https://github.com/MrMirhan/intellyrouter/actions/workflows/ci.yml/badge.svg)](https://github.com/MrMirhan/intellyrouter/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/MrMirhan/intellyrouter.svg)](https://pkg.go.dev/github.com/MrMirhan/intellyrouter)
[![Go 1.27](https://img.shields.io/badge/go-1.27-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue)](./LICENSE)

IntellyRouter is a single Go binary that proxies `/v1/messages` between
Claude Code and one or more model providers. Cheap models do the work.
The Claude subscription steps in when the cheap model flags trouble, when
the user asks for a review, or when an image lands. Nothing is hardcoded:
providers, models, routes, tiers, escalations, advisors, classifiers —
all of it lives in the dashboard and survives in SQLite.

The gateway is local-first. It binds to `127.0.0.1:7117` by default,
runs in front of Claude Code on the same machine, and exposes a
dashboard on the same port for the operator. Docker and Coolify / Dokploy
deploys work, but the gateway never originates calls to subscription
upstreams from a container — see [Deploy](#deploy).

## Features

- **Direct, escalate, and guided routes.** A direct route is one tier.
  An escalate route picks a tier per request from markers (`#<label>`,
  `#up`, `#base`), a classifier call, or a failure-streak signal. A
  guided route adds a director that reads the executor's session at
  checkpoints and writes guidance the client never sees.
- **Combo models.** Combine several models as `combo/<name>` and pick
  fallback, round-robin, or least-used as the strategy. Combos can be
  tiers of another route.
- **Advisor per route.** A separate model the executor consults mid-run
  through a hidden tool call. The advisor's answer stays in the
  executor's history after the hidden exchange ends.
- **Image-aware tier selection.** A request carrying an image block
  goes to a tier whose model reads images, with a fallback to lower
  tiers when no higher tier does.
- **Local-first storage.** SQLite (modernc.org/sqlite) with API keys
  encrypted under `master.key` next to the data directory. AES-GCM.
- **Embedded dashboard.** React + Vite + shadcn, built with `bun run
  build` and embedded into the binary with `-tags webui`.
- **Eval runner.** `cmd/intelly-eval` drives Claude Code through the
  gateway against the tasks in `eval/tasks`, with grading by each
  task's reference tests.

## Quick start

```sh
git clone https://github.com/MrMirhan/intellyrouter
cd intellyrouter
go test ./...
go build -tags webui -o intellyrouter ./cmd/intellyrouter
./intellyrouter -addr 127.0.0.1:7117 -data ~/.intellyrouter
```

Open `http://127.0.0.1:7117/`, paste the admin token the binary prints at
first start, and add a provider. The dashboard walks you through the
rest.

To run Claude Code through the gateway:

```sh
export ANTHROPIC_BASE_URL=http://127.0.0.1:7117
export ANTHROPIC_CUSTOM_HEADERS="x-intelly-key: <paste the gateway key>"
claude
```

The gateway key is at `http://127.0.0.1:7117/keys`, not the admin token.

## Install

### `go install`

```sh
go install github.com/MrMirhan/intellyrouter/cmd/intellyrouter@latest
go install github.com/MrMirhan/intellyrouter/cmd/intelly-eval@latest
```

Requires Go 1.27+. The `go install` build does not embed the web
dashboard — it skips the `webui` build tag, so the binary serves the
gateway API on `/api/hello` and `/v1/messages` but not the dashboard at
`/`. Build from source with `-tags webui` (see below) when you want the
dashboard.

### Build from source

```sh
git clone https://github.com/MrMirhan/intellyrouter
cd intellyrouter
make build
```

`make build` builds the gateway with `-tags webui` after running
`bun install` and `bun run build` in `web/`. The Makefile runs the same
checks CI does (`go vet ./...`, `go test ./...`).

### Docker

```sh
docker run --rm -d --name intellyrouter \
  -p 127.0.0.1:7117:7117 \
  -v intellyrouter-data:/data \
  -v "$PWD/eval/tasks:/eval-tasks:ro" \
  ghcr.io/mrmirhan/intellyrouter:dev
```

The image bundles the web dashboard and the eval runner. Mount `/data`
on a persistent volume so `master.key` and `intellyrouter.db` survive
restarts; without it, every restart re-encrypts provider keys under
a new master key.

### Build from source

```sh
git clone https://github.com/MrMirhan/intellyrouter
cd intellyrouter
make build
```

The Makefile builds the gateway with `-tags webui` and runs the same
checks CI does (`go vet ./...`, `go test ./...`).

## Deploy

The gateway *relays* subscription traffic end-to-end. Claude Code's
own `/login` session reaches the upstream unchanged — the gateway only
rewrites the `model` field, never the `Authorization` header. This
works the same on a host, on Coolify, and on Dokploy.

What does **not** work from a container, because the container has no
Claude Code to log in with, is the parts of the gateway that originate
their own subscription calls:

- **Claude-Code director** (`DirectorSettings.ClaudeCode: true`):
  the gateway shells out to `claude` with the captured session. Inside
  a container there is no `~/.claude/` to read; install Claude Code
  in the image or use an API-key director instead.
- **Subscription advisor**: same `claude` requirement.
- **Eval runner** (`cmd/intelly-eval`): spawns `claude -p` against a
  logged-in session. Run it on the host.

Subscription routes and the eval runner's `claude` calls are the only
parts that need Claude Code installed; everything else (subscription
passthrough, API-key directors, advisor on API-key models, classifier,
escalation tiers, combo strategies) is fine inside a container.

`docker-compose.yml` is the Coolify / generic entry; `docker-compose.dokploy.yml`
exists for per-app Dokploy stacks that want a different image tag.

## Configuration

- **Flags** — `-addr`, `-data`, `-eval-tasks`, `-claude` (the binary
  to use for eval runs and for `viaClaudeCode` directors), and
  `-reset-admin-token` to rotate the admin token printed at first
  start.
- **Env** — `INTELLYROUTER_ADDR`, `INTELLYROUTER_DATA`,
  `INTELLYROUTER_EVAL_TASKS`. The Docker image picks them up by
  default; see `.env.example`.
- **Dashboard** — Providers, Models, Routes, Combos, Keys, Sessions,
  Settings. All persisted in SQLite.
- **Gateway key** — a static API key Claude Code presents as
  `x-intelly-key`. Distinct from the admin cookie.

## Architecture

See [ARCHITECTURE.md](./ARCHITECTURE.md) for the layered diagram,
package table, and one request's path through route → tier → legs
→ ledger.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md). One commit per logical change,
one-line commit messages with a semantic type, no AI-tool trailers.

## Security

See [SECURITY.md](./SECURITY.md). Subscription tokens are never stored
or originated by the gateway; API keys are encrypted at rest.

## License

[Apache-2.0](./LICENSE).
