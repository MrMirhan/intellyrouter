# Architecture

`intellyrouter` is a single Go binary that sits between Claude Code and
one or more model providers; the dashboard is bundled into the same
binary with `webui` build tag and served from `internal/dashboard`.
The code is layered top-to-bottom with strict directional dependencies:

```
                                  ┌─────────────────────────────┐
HTTP /v1/messages  ─────►         │  internal/gateway            │
HTTP /v1/models    ─────►         │  ─ messages.go (Anthropic    │
HTTP /v1/embeddings ─────►         │     format parsing, SSE      │
                                  │     relay, compat.shim)      │
                                  │  ─ guided.go                 │
                                  │  ─ escalate.go               │
                                  │  ─ advisor.go                │
                                  │  ─ contextfit.go (vision +   │
                                  │     context fit)             │
                                  │  ─ combo.go (fallback / RR /  │
                                  │     least-used)              │
                                  └──────────┬──────────────────┘
                                             │
                                             ▼
                                  ┌─────────────────────────────┐
                                  │  internal/guided             │
                                  │  — what to do on each turn:  │
                                  │    inject director guidance  │
                                  │    and advisor answers, ask  │
                                  │    the executor, classify    │
                                  └──────────┬────────┬─────────┘
                                             │          │
                                             ▼          ▼
                                  ┌─────────────┐  ┌─────────────────────┐
                                  │  internal/  │  │  internal/escalate  │
                                  │  ledger     │  │  — failure streaks, │
                                  │  — every    │  │    token-window    │
                                  │    upstream │  │    fits, escalation │
                                  │    call re- │  │    marks, cost per │
                                  │    corded    │  │    request         │
                                  └──────┬──────┘  └─────────┬──────────┘
                                         │                  │
                                         ▼                  ▼
                                  ┌─────────────────────────────┐
                                  │  internal/store              │
                                  │  — SQLite (modernc.org/      │
                                  │    sqlite), AES-GCM API      │
                                  │    keys, request/leg rows,  │
                                  │    sessions, combos,         │
                                  │    settings                  │
                                  └──────────┬──────────────────┘
                                             │
                                             ▼
                                  ┌─────────────────────────────┐
                                  │  internal/provider           │
                                  │  — Anthropic-format upstream │
                                  │    URLs (Anthropic,          │
                                  │    Anthropic-compatible,     │
                                  │    OpenRouter, Anthropic-   │
                                  │    subscription), OpenAI     │
                                  │    translation shim          │
                                  └──────────┬──────────────────┘
                                             │
                                             ▼
                                  ┌─────────────────────────────┐
                                  │  web/                        │
                                  │  — React + Vite + shadcn,    │
                                  │    embedded at build with    │
                                  │    `-tags webui` (see         │
                                  │    web/embed_webui.go)       │
                                  └─────────────────────────────┘
```

## Packages

| Package | Purpose |
| --- | --- |
| `cmd/intellyrouter` | Entrypoint. Parses flags (`-addr`, `-data`, `-eval-tasks`, `-reset-admin-token`, `-claude`), opens the store, starts the gateway, dashboard, eval manager. |
| `cmd/intelly-eval` | Eval runner. Spawns `claude -p` against the gateway, grades with the task's tests, writes the report. Real provider calls. |
| `internal/gateway` | HTTP server. Anthropic format passthrough with byte-preserving SSE relay, `model` rewrite, prompt-cache friendly injection. |
| `internal/gateway/messages.go` | Request parsing, response streaming, usage capture. |
| `internal/gateway/guided.go` | A director that reads the executor's session, writes guidance, optionally takes the step itself when the director is a Claude subscription model. |
| `internal/gateway/escalate.go` | Per-turn tier selection, retry-and-fallback when the upstream rejects the prompt as too long. |
| `internal/gateway/advisor.go` | Hides the advisor exchange in the executor's stream, so the executor keeps the answer after the hidden turn ends. |
| `internal/gateway/contextfit.go` | Routes an image request to a tier whose model reads images; drops down when no higher tier accepts images. |
| `internal/gateway/combo.go` | Member ordering for fallback, round-robin, and least-used strategies. |
| `internal/guided` | The director's system prompt, guidance injection, advisor answer injection, prompt classification (unsure, review, repeat). Vision detection lives here. |
| `internal/escalate` | Failure-streak and context-fit signals, turn-key derivation. |
| `internal/store` | SQLite (modernc.org/sqlite), schema migrations, request/leg inserts, session summaries, combo CRUD, settings. AES-GCM encryption of provider API keys. |
| `internal/ledger` | Per-leg record: provider, model, billing (subscription or api), token counts, cost, latency, status, note. |
| `internal/provider` | Provider type registry, Anthropic-format request byte rewriting, OpenAI translation shim. |
| `internal/dashboard` | Embeds the web/ build, serves `/`, the admin API at `/api/admin/*`, and the gateway API at `/api/hello`, `/v1/*`. |
| `web/` | React + Vite + shadcn. Static assets produced by `bun run build` and embedded via `-tags webui`. |

## One request, end to end

A Claude Code request lands at `/v1/messages`. `internal/gateway/messages.go`
parses the Anthropic-format body, decides the route (direct, escalate,
guided), and constructs a `clientRequest`.

```
guided (the interesting path)
─────────────────────────────────────────────────────────────

messages.go → guided.Analyze(body)
   │
   ▼
guided.State is keyed by session id + the SHA-256 of the prompt index.
First call in a turn hits guided.go's checkpoint logic:

  - dec.Reason != "" && director is a Claude subscription model:
        the gateway calls the director itself (via CLI passthrough) and
        answers the client. leg.role = director_step. turn ends here.
  - dec.Reason != "" otherwise:
        consult director → st.Guidance, st.GuidanceReason. leg.role = director.
  - AskUserQuestion / repeat / failed-tools signals become the next dec.Reason.

Then fittingTier(start, size, needsVision) chooses the executor tier:

  - needsVision && !tier.Model.Vision → skip
  - !fits(size, tier.Model.Context)  → skip
  - if start..end has no tier, look below (because a lower tier that reads
    images is still better than a higher tier that cannot).

executor's request body ← guided.InjectGuidance(body, st.Guidance, ...)
   │
   ▼
call(executor) records:
   leg.role   = executor
   leg.model  = MiniMax-M3
   leg.billing= api
   leg.tokens = (input, output, cache_read, cache_write)
   leg.cost   = combination of price_in / price_out / price_cache_*
   leg.note   = "tier MiniMax-M3; guidance from failed tool results"
   │
   ▼
if held.overflow() → largerTier → retry on the next tier
if body carried an image and the tier reads images → no move
otherwise                                               → "tier MiniMax-M3"

ledger sums legs into a request row and updates the request's running
totals. Sessions page reads those via sessionSummaries(), which joins
workTotalsBySession and directorModelsBySession.
```

Subscription traffic has its own path: the gateway only rewrites the
`model` field, never the `Authorization` header. Claude Code's `/login`
session token reaches the upstream unchanged. The gateway never makes
its own call with a subscription token — that path is forbidden by
`internal/gateway/messages.go` and tested in `subscription_test.go`.
