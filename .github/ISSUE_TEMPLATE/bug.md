---
name: Bug report
about: Something in the gateway does the wrong thing
title: "[bug] "
labels: ["bug", "needs-triage"]
assignees: []
---

## What happened

A clear, one-line description of the bug.

## Steps to reproduce

1. `intellyrouter -addr 127.0.0.1:7117 -data /tmp/ir`
2. Open the dashboard, add a provider, ...
3. ...

## What I expected

What should have happened instead.

## What I saw

The actual behaviour: an error message, the wrong model used, a
session that didn't get logged. Paste leg notes from
`/api/admin/sessions/<id>` if the bug is in routing.

## Versions

- The git commit you built from (`git rev-parse HEAD`).
- Go version (`go version`).
- Web dashboard version (shown in the footer).
- Provider models involved.

## Configuration

- Number of providers and routes.
- Whether the route uses guided, escalate, or direct.
- Whether `capture_content` is on or off.
