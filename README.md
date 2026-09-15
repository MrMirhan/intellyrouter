# IntellyRouter

IntellyRouter is a local gateway for Claude Code. Low-cost models do most of the work. A stronger Claude model directs them at checkpoints, or takes a turn when you ask for it, when a small classifier finds that you are not satisfied, or when the work fails several times in a row. The gateway records every upstream call, so the dashboard shows what each request cost and how much you saved.

## How it works

Claude Code sends Anthropic Messages API requests to the gateway. The model name in each request selects a route. A route has a strategy and an ordered list of models (tiers).

- `direct` sends every request to one model.
- `escalate` sends a turn to tier 0, the low-cost base model, unless one of these triggers selects a higher tier:
  - A marker in your prompt: `#<label>`, `#up`, or `#base`.
  - The classifier: a small model reads your latest message once per turn. It escalates when you reject the previous result, ask for a review, or ask for a stronger model.
  - A failure streak: a number of failed tool calls in a row.
- `guided` sends every request to a low-cost executor model. A director model, for example Claude Fable 5.1, reads the session at checkpoints and tells the executor what to do next. See [Guided routes](#guided-routes).

On an escalate route, all requests of one turn (the tool loop after one prompt) go to the same tier.

The gateway supports these provider types:

| Type | Format | Credential |
| --- | --- | --- |
| `anthropic` | Anthropic, passed through unchanged | API key |
| `anthropic-compatible` | Anthropic, passed through unchanged (DeepSeek, Kimi, GLM, MiniMax) | API key |
| `anthropic-subscription` | Anthropic, passed through unchanged | Your own Claude Code login |
| `openrouter` | Anthropic, passed through unchanged | API key |
| `openai` | OpenAI Chat Completions, translated | API key |
| `openai-compatible` | OpenAI Chat Completions, translated (OrcaRouter, Gemini, Ollama) | API key, optional |

### Claude subscription

The gateway never asks for your Claude login and never stores it. Claude Code signs in with `/login` as usual. On a route tier that uses an `anthropic-subscription` provider, the gateway sends Claude Code's own request to Anthropic, changes only the model name, and keeps the `Authorization` header. For all other providers, the gateway removes that header.

Calls that the gateway starts itself, such as the classifier and director checkpoints, never use the subscription. Anthropic does not permit third-party tools to collect or reuse Claude.ai credentials, so do not change this behavior.

## Requirements

- Go 1.27.1 or later. The `go.mod` file selects this toolchain.
- Bun 1.3 or later, to build the dashboard.
- Claude Code.

## Build and run

```sh
make build
./bin/intellyrouter
```

On the first start, the gateway prints an admin token one time. Keep it. To issue a new token, run `./bin/intellyrouter -reset-admin-token`.

Open http://127.0.0.1:7117 and sign in with the admin token.

| Flag | Default | Purpose |
| --- | --- | --- |
| `-addr` | `127.0.0.1:7117` | Listen address |
| `-data` | `~/.intellyrouter` | Database and master key directory |
| `-reset-admin-token` | `false` | Issue and print a new admin token |
| `-eval-tasks` | `eval/tasks` | Directory with eval tasks for the dashboard |
| `-claude` | `claude` | Claude Code binary that eval runs start |

For development, run the Go server with `go run ./cmd/intellyrouter` and the dashboard with `cd web && bun run dev`. The Vite dev server sends `/api` and `/v1` requests to the Go server.

## Set up

1. **Add providers.** On the Providers page, select a preset or enter a type, a base URL, and an API key.
2. **Get models.** Select "Sync models". If a provider has no model list, add models by hand. Enable the models that you want to use and check their prices. Prices are in USD per one million tokens.
3. **Create a route.** On the Routes page, give the route a name that contains `claude`, so that Claude Code can discover it. For example, create `intelly-claude-auto` with the `escalate` strategy, `deepseek-v4-flash` as tier 0 (label `flash`), and `claude-opus-5` on your subscription as tier 1 (label `opus`).
4. **Create a gateway key.** On the Keys page, create a key. The dashboard shows the key one time.
5. **Connect Claude Code.** Use the snippet on the Routes page.

With a gateway key only:

```sh
export ANTHROPIC_BASE_URL=http://127.0.0.1:7117
export ANTHROPIC_AUTH_TOKEN=<gateway key>
export ANTHROPIC_MODEL=intelly-claude-auto
export ANTHROPIC_DEFAULT_OPUS_MODEL=intelly-claude-auto
export ANTHROPIC_DEFAULT_SONNET_MODEL=intelly-claude-auto
export ANTHROPIC_DEFAULT_HAIKU_MODEL=intelly-claude-auto
export CLAUDE_CODE_SUBAGENT_MODEL=intelly-claude-auto
```

With your Claude subscription on some tiers, do not set `ANTHROPIC_AUTH_TOKEN` or `ANTHROPIC_API_KEY`. Send the gateway key in a separate header, then run `/login` in Claude Code:

```sh
export ANTHROPIC_BASE_URL=http://127.0.0.1:7117
export ANTHROPIC_CUSTOM_HEADERS="x-intelly-key: <gateway key>"
export ANTHROPIC_MODEL=intelly-claude-auto
```

Claude Code also sends background and subagent requests. Map the `ANTHROPIC_DEFAULT_*_MODEL` and `CLAUDE_CODE_SUBAGENT_MODEL` variables to a route, or those requests fail with 404.

Claude Code does not know route names, so it compacts the conversation at 200K tokens. When every model in the route has a 1M window, add `[1m]` to the name, for example `ANTHROPIC_MODEL='intelly-claude-auto[1m]'`. Claude Code removes the suffix before it sends the request and compacts near 1M. The connect snippet on the Routes page adds the suffix when "1M context window" is on.

If a non-Claude provider rejects Claude Code beta fields with a 400 error, set `CLAUDE_CODE_DISABLE_EXPERIMENTAL_BETAS=1`.

## Escalation rules

An escalate route stores its rules in the route settings:

```json
{
  "classifier": { "enabled": true, "model_id": 12, "target": "top" },
  "failure_streak": { "enabled": true, "threshold": 3, "target": "next" }
}
```

- `target: "next"` moves up one tier. `target: "top"` moves to the last tier.
- The classifier model must be enabled. It cannot use a subscription provider.
- Defaults: the classifier is off, and the failure streak is on with a threshold of 3.

| Marker | Effect for the current turn |
| --- | --- |
| `#<label>` | Use the tier with that label |
| `#up` | Use tier 1 |
| `#base` | Use tier 0 |

An escalated turn sends the full conversation to the Claude model. That model has no cache for the turns that a different model served, so the turn uses more of your plan limit than a normal Claude turn. Escalate when the result matters.

## Guided routes

On a guided route, low-cost executor models read files, run commands, and write the code. A stronger director model steers them and does not write the final output. Claude Code does not see this process.

At a checkpoint, the gateway sends the director a text copy of the session: the prompts, the assistant text, the tool calls, and shortened tool results. Thinking blocks are not sent. The director replies with short guidance. The gateway adds the guidance to the last user message of the executor request, and keeps it for the next requests of the same turn.

| Checkpoint | When it occurs |
| --- | --- |
| `turn_start` | You send a new prompt. |
| `failed_results` | The session has this number of new failed tool results since the last check. |
| `unsure` | The executor writes that it is stuck or not sure. |
| `review_on_success` | Files changed and the latest tool results pass. The director approves the work or lists what to fix. |
| `steps` | The executor made this number of steps since the last check. |

Only one checkpoint occurs for each executor step. The route settings:

```json
{
  "director": { "model_id": 2, "effort": "medium", "max_calls_per_turn": 6 },
  "checkpoints": { "turn_start": true, "failed_results": 2, "steps": 15, "unsure": true, "review_on_success": true },
  "escalate_after": 2,
  "consult": true
}
```

- A count of 0 turns that checkpoint off.
- `max_calls_per_turn` limits the director calls for one prompt and its tool loop.
- `escalate_after`: after this number of failure checkpoints in one turn, the next requests go to the next executor. 0 keeps the first executor.
- If a director call fails, the executor continues with the previous guidance.
- A director on an `anthropic-subscription` provider: the gateway cannot call it on its own. At a checkpoint, the gateway sends Claude Code's request to the director, so the director does that step itself. The executors get no written guidance.
- An executor on an `anthropic-subscription` provider gets no guidance, because the gateway changes only the model name in subscription requests.

### Questions from the executor

With `"consult": true` (the default), the executor gets one more tool, `ask_director`. When the executor calls it, the gateway sends the question and the session to the director, adds the answer to the executor's request, and continues the executor's response. Claude Code receives one message and does not see the question or the answer. The gateway sends pings while the director works.

- This works only for streaming requests, with a director and an executor that use API keys.
- A question uses one director call of `max_calls_per_turn`. One request can have at most 3 questions.
- If the executor calls `ask_director` together with other tools, Claude Code runs the other tools. The answer goes to the executor as guidance in the next request.

## Costs and savings

- **API spend**: the cost of all calls that your providers bill.
- **Subscription value**: Claude subscription usage at API prices. It costs you nothing extra, but it uses your plan limits. The dashboard also shows the latest rate-limit headers from Anthropic.
- **Estimated savings**: the cost of the base-tier tokens at the price of the route's top model (the director model for guided routes, or the reference model for direct routes), minus the API spend. Classifier and director calls show as routing overhead. Different models use different numbers of tokens and turns, so this value is an estimate. Use the eval runner for a measured comparison.

## Eval

`eval/tasks` holds coding tasks with tests. To check that each task fails before its reference fix and passes after it, run `eval/tasks/validate.sh`.

An eval run starts headless Claude Code on each selected task through the gateway, one time for each selected route. It restores the protected test files, runs the tests, and reads the cost from the ledger. Use it to compare, for example, a low-cost model alone, Opus 5 alone, and an escalate route.

In one-shot tasks, nobody tells the model that a result is not good enough, so the classifier seldom escalates. An escalate route in an eval shows mostly the quality of the base model and the failure-streak escalation.

### From the dashboard

On the Eval page, select tasks, routes, a mode, and the number of runs at the same time, confirm the warning, and start the run. You can follow the progress, cancel the run, and compare the routes when it ends. Only one run can be active. A route that uses an `anthropic-subscription` model, as a tier or as the director, needs subscription mode: gateway key mode starts Claude Code without your Claude login, so the gateway does not start that run. The gateway reads the tasks from `-eval-tasks` (default `eval/tasks`) and starts the Claude Code binary from `-claude` (default `claude`).

### From the command line

```sh
go run ./cmd/intelly-eval \
  -key <gateway key> -admin-token <admin token> \
  -routes intelly-claude-auto,intelly-claude-opus \
  -mode subscription -yes
```

Without `-yes`, the command prints the plan and stops.

> **Warning:** Each run lets a model edit files and run `go` or `python3` commands on this computer, in a temporary copy of the task. API calls cost money, and subscription mode uses your plan limits.

## Security

- The gateway listens on `127.0.0.1` by default. It has no TLS. If you use a different address, put a TLS proxy in front of it.
- Provider API keys are encrypted with AES-256-GCM. The master key is in `<data>/master.key` (file mode 0600), or in the `INTELLY_MASTER_KEY` environment variable as base64.
- The database stores only SHA-256 hashes of gateway keys and the admin token.
- The dashboard uses an HttpOnly, SameSite=Strict session cookie and a strict Content Security Policy. Admin API calls that change data must use `Content-Type: application/json`.

## Known limits

- For OpenAI-format providers, the translation removes thinking blocks, `cache_control` markers, and server tools. Token counting is not available on those routes, so Claude Code uses its own estimate.
- Anthropic does not support Claude Code with non-Claude models. Check a model with the eval runner before you depend on it.
