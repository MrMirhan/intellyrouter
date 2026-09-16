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

## Direct model names and combos

Every provider has a slug. Claude Code can use an enabled model without a route as `<slug>/<model id>`, for example `cc/claude-opus-5` when the Claude subscription provider has the slug `cc`, or `gemini/gemini-3.8-flash`. The gateway sends the request to that model, like a direct route. The slug is the provider name in lowercase with dashes unless you set one in the provider's settings. A `[1m]` suffix works as for routes, for example `cc/claude-opus-5[1m]`.

A combo is a named list of models, on the Combos page. Each request goes to one of its models:

- **Fallback chain**: the first model. The next model gets the request only when the one before it fails.
- **Round robin**: the models take turns.
- **Least used**: the model with the fewest requests in progress.

With every strategy, a request that fails before its response starts moves on to the next model: after a rate limit, a server error, a rejected key or model, or a prompt too long for the model's context window. A request that the upstream finds invalid goes back to Claude Code with that error.

Use a combo in Claude Code as `combo/<name>`, or choose it in a route like a model: as a tier, the director, the advisor, or the classifier.

- A combo cannot contain another combo.
- The gateway does not use a Claude subscription for its own calls, so a director, advisor, or classifier combo skips its subscription models.
- A guided route treats a combo that contains a subscription model like a subscription tier: the executor gets no written guidance and no questions to the director or the advisor.
- The ledger records the model that answered, with a note such as "combo stack after MiniMax-M2.7 returned 429".
- `/v1/models` lists the direct names. Claude Code's model discovery keeps only names that contain `claude` or `anthropic`; add other names to the `/model` picker with `modelPicker`.

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
| `turn_start` | You send a new prompt. Skill content, local command output (such as `/model`), and hook feedback are not prompts. |
| `failed_results` | The session has this number of new failed tool results since the last check. |
| `repeats` | The executor makes the same tool call with the same result this number of times. |
| `unsure` | The executor writes that it is stuck or not sure. |
| `review_on_success` | Files changed and the latest test, lint, or check command passes. The director approves the work or lists what to fix. |
| `steps` | The executor made this number of steps since the last check. Off by default: the other checkpoints cover long work at a lower cost. |

Only one checkpoint occurs for each executor step. The route settings:

```json
{
  "director": { "model_id": 2, "effort": "medium", "max_calls_per_turn": 6 },
  "checkpoints": { "turn_start": true, "failed_results": 2, "repeats": 3, "unsure": true, "review_on_success": true, "steps": 0 },
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

### Director through Claude Code

A director on an `anthropic-subscription` provider can still give written guidance and answer `ask_director` questions. Turn on "Ask through Claude Code" in the route's director settings. For each checkpoint and question, the gateway starts the Claude Code CLI on its own machine and waits for the answer:

```sh
claude -p --model <director model> --tools "" --append-system-prompt "<director instruction>" \
  --output-format json --setting-sources project --strict-mcp-config --effort <effort> \
  --session-id <id>   # or --resume <id> for later checkpoints
```

- Claude Code uses your own Claude login on the gateway machine. The gateway does not read the login. It removes `ANTHROPIC_*` and `CLAUDE_CODE_*` variables from the process, so the call does not go to a gateway or an API key. It sets `CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`, so Claude Code does not send the prompt again for a session title.
- Claude Code keeps its own system prompt and adds the director instruction. The director has no tools.
- The first call of an executor session sends the session's system text, the messages, and the checkpoint. Later calls resume the same Claude Code conversation and send only the new messages, so Claude Code reads the earlier part from its prompt cache. When the session no longer starts with what the director read (for example after a compaction), the gateway starts a new conversation.
- Director conversations run in `<data>/claude-director`. Claude Code saves them under `~/.claude/projects/`; the gateway deletes a conversation when it starts a new one or after one idle hour.
- A director model with a context window larger than 200K runs as `<model>[1m]`.
- At most two of these calls run at the same time. Each call uses your plan limits. The ledger records it as a subscription director leg with the note "via Claude Code".
- The gateway starts Claude Code with the `-claude` flag of `intellyrouter` (default `claude`). If the call fails, the executor continues with the previous guidance.

### Advisor

A route can have an advisor: a second model that the executor asks when it is stuck, when an error keeps coming back, when it is not sure which approach is correct, before a large change, and before it reports that the task is done. Choose the advisor model in the route editor. The model can be on any provider. For example, a guided route can use DeepSeek V4.1 Flash as executor, GLM 5.3 as advisor, and Claude Opus 5 as director.

- The gateway adds an `ask_advisor` tool to the executor's request. When the executor calls it, the gateway sends the advisor a text copy of the session and the question, adds the answer, and continues the same response. Claude Code does not see the question. Later requests in the same turn carry the latest answer.
- The advisor works on direct, escalate, and guided routes, for streaming requests with tools. On a guided route with an advisor, the executor asks the advisor instead of the director. The director still handles the checkpoints.
- An advisor on an `anthropic-subscription` provider answers through the Claude Code CLI on the gateway machine, like a [director through Claude Code](#director-through-claude-code), and uses your plan limits.
- "Questions per turn" limits the answers in one turn (default 6). One request continues after at most 3 questions. The ledger records each answer as an Advisor leg, and the overview counts its API cost as routing overhead.
- A step on your Claude subscription cannot get `ask_advisor`, because the gateway cannot continue a subscription response on its own. On those steps Claude Code's own advisor tool can run. Anthropic runs that tool, so it needs a Claude advisor model, and Claude Code must have the advisor on. Choose it in the connect snippet, or set these values yourself (the model ID is an example):

  ```json
  {
    "advisorModel": "claude-opus-5",
    "env": { "CLAUDE_CODE_ENABLE_EXPERIMENTAL_ADVISOR_TOOL": "1" }
  }
  ```

  Claude Code adds its advisor tool to a request for a route name only when `CLAUDE_CODE_ENABLE_EXPERIMENTAL_ADVISOR_TOOL=1` is set.
- When the route's advisor is a Claude model, the gateway puts that model into Claude Code's advisor tool. On a step on an Anthropic API key where Claude Code sends that tool, Anthropic runs it, and the gateway does not add `ask_advisor`. When the route's advisor is on another provider, the gateway removes Claude Code's advisor tool. Off removes Claude Code's advisor tool and adds no advisor.
- When a model rejects Claude Code's advisor tool with a 400 error, the gateway sends that request again without the tool and remembers this for that model. Anthropic reports those advisor tokens apart from the main call, and the ledger records them as an Advisor leg.

## Context windows

Claude Code sends the whole conversation in every request, and the gateway passes it to the model of the step. A model with a smaller context window than the conversation rejects the request. On escalate and guided routes the gateway prevents this:

- Before a step, the gateway estimates the size of the request. It learns the tokens per byte of each session from the usage that providers report. If the model's context window cannot hold the request, the step goes to the next tier that can, and the leg note gives the reason.
- If the estimate is too low and the provider rejects the prompt as too long, the client does not see that error. The gateway sends the request again to the next tier with a larger or unknown context window.
- The gateway reads the context window from the model's `context` field on the Providers page. Syncing fills it when the provider reports it. A model without a value counts as large enough.
- Claude Code compacts the conversation at the window it assumes for the route name: 200K tokens, or 1M with the `[1m]` suffix. With `[1m]`, a route whose last tier has a smaller window than the conversation still fails, because no tier can hold the request.

## Images

A model that reads images has "Supports images" set on the Providers page. A request that carries an image or a document block goes to a tier with that setting:

- The move applies to that request only. The turn keeps its own tier, so the next request without an image goes back to it.
- The gateway first looks at the tiers above the turn's tier. If none of them takes images, it looks below, because a lower tier that reads the image is better than a higher tier that cannot.
- A tier must also hold the request. A tier that takes images but has too small a context window does not serve it.
- A combo has no setting of its own. It takes images when one of its models does.
- When no tier takes images, the request goes to the last tier and the leg note says so. The model then answers as if the image were not there, or rejects it.

## Costs and savings

- **API spend**: the cost of all calls that your providers bill.
- **Cache writes**: a one-hour cache write costs two times the input price, and a five-minute cache write uses the model's cache write price. The ledger prices each write by its TTL.
- **Subscription value**: Claude subscription usage at API prices. It costs you nothing extra, but it uses your plan limits. The dashboard also shows the latest rate-limit headers from Anthropic.
- **Routing vs one model**: the Overview and each session price the work tokens at one model, for example Claude Fable 5.1, and compare that with what the routing actually used (API spend plus subscription value). Work tokens are the calls that answered the client: the classifier, the advisor, and the director's checkpoint calls are routing overhead and do not count, but a director that answers a step itself does. A single model would use a different number of tokens and turns, so this value is an estimate.
- **Saved**: the Sessions list prices each session's work tokens at the model that served most of its director calls, and shows the gap to what the session spent. The session detail opens the same comparison on that model. A session with no director call uses the reference model from the settings.
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

## Sessions and content capture

The Sessions page groups requests by the Claude Code session ID. A session shows which models did the work, the director checkpoints, the token use, and the comparison with one model.

By default the gateway stores only request metadata: models, tokens, costs, latency, and notes. To keep the prompts and responses too, turn on content capture in Settings. Then each request shows the messages that Claude Code sent, the response, and the text that the gateway added (director guidance and checkpoint prompts), and you can export a request as JSON or a session as JSONL.

- Claude Code sends the full conversation in every request. The gateway stores each message one time and reuses it, so a long session does not store the same history again for each request.
- The gateway deletes captured content after the retention period (default 14 days). The request metadata stays.

> **Warning:** Content capture stores prompts, code, tool output, and responses in the database in the data directory, without encryption. Keep it off for sensitive work, and protect the data directory.

## Security

- The gateway listens on `127.0.0.1` by default. It has no TLS. If you use a different address, put a TLS proxy in front of it.
- Provider API keys are encrypted with AES-256-GCM. The master key is in `<data>/master.key` (file mode 0600), or in the `INTELLY_MASTER_KEY` environment variable as base64.
- The database stores only SHA-256 hashes of gateway keys and the admin token.
- Content capture is off by default. When it is on, the database also stores prompts and responses without encryption.
- The dashboard uses an HttpOnly, SameSite=Strict session cookie and a strict Content Security Policy. Admin API calls that change data must use `Content-Type: application/json`.

## Known limits

- For OpenAI-format providers, the translation removes thinking blocks, `cache_control` markers, and server tools. Token counting is not available on those routes, so Claude Code uses its own estimate.
- Anthropic does not support Claude Code with non-Claude models. Check a model with the eval runner before you depend on it.
