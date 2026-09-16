# Security Policy

## Reporting a Vulnerability

If you find a security issue, **do not open a public GitHub issue**. Email
the maintainers at the address listed on the GitHub profile of the
project owner, or open a [GitHub private security advisory][advisory] on
this repository. We will respond within 72 hours.

[advisory]: https://docs.github.com/en/code-security/security-advisories/guidance-on-repository-security-advisories

## What to Expect

- An acknowledgment within 72 hours.
- A triage on whether the report is in scope, with a status update within
  seven days.
- A patch or a workaround before public disclosure. We coordinate
  disclosure timing with reporters.

## Scope

- The Go binary (`cmd/intellyrouter`, `cmd/intelly-eval`) and all
  packages under `internal/`.
- The web dashboard bundled into the binary with `-tags webui`. Source
  lives in `web/`.
- The SQLite store at the path given to `-data`. It is local to the host
  by default.

Out of scope:

- Provider-specific behaviour (rate limits, content policy, prompt
  injection against third-party models). Report those to the provider.
- Anything upstream of Claude Code / OpenCode / Cursor / Codex
  themselves.

## Threat Model

The gateway sits between an AI coding agent and one or more model
providers. It is intended to run on the same machine as the agent and
to bind to localhost, or to bind to a private interface behind a
reverse proxy. It does not implement authentication beyond a static
API key for the agent and an admin cookie for the dashboard.

- The gateway never stores the user's Claude subscription OAuth token.
  Subscription traffic goes through the agent's own `/login` session,
  with only the model name rewritten by the gateway.
- Provider API keys live in SQLite encrypted with AES-GCM under a key
  in `master.key` next to the data directory. The key is generated on
  first run; copy it somewhere safe.
- The dashboard does not log request or response bodies by default.
  Set `capture_content` in the settings table to record bodies for
  debugging. Disable it again before sharing the data directory.

## Operator Notes

- Bind to `127.0.0.1` for single-host use, or run behind a TLS-terminating
  proxy. The gateway logs a warning at startup if bound to a non-loopback
  address without TLS.
- Audit `/api/admin/requests` and the leg notes for unexpected keys,
  billing, or models.
- Rotate `master.key` to re-encrypt API keys: stop the gateway, copy
  `master.key` aside, delete it, restart and re-paste the API keys via
  the dashboard. The store re-encrypts under the new key.
- The eval runner `cmd/intelly-eval` makes real provider calls. Run it
  only with provider accounts you control and against model pricing you
  have read.
