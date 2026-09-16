# Contributing to IntellyRouter

Thanks for taking the time to contribute. The project is small and the
expectations are clear.

## Setup

- Go 1.27 or newer.
- Bun 1.x for the embedded dashboard (Node 20+ also works).
- A POSIX shell.

```sh
go test ./...
cd web && bunx tsc --noEmit -p tsconfig.app.json && bun run lint && bun run build
```

`go test ./...` covers the backend. The frontend ships as static assets
embedded into the binary with `-tags webui`, so a build failure in `web/`
will fail the gateway's `webui` build too. Build the gateway the same way
CI does:

```sh
go build -tags webui ./cmd/intellyrouter
```

## Issues

- **Bugs** — include the git commit you built from, the request that
  triggered it, and the relevant leg notes from `/api/admin/sessions/<id>`.
- **Features** — open the discussion before opening a PR. Many features
  touch the gateway and the dashboard together; a draft UI matters more
  than a sketch in prose.

## Pull requests

- One commit per logical change. `git commit --amend` is fine; force-push
  your branch until the conversation is on the PR review.
- One line commit message with a semantic type
  (`feat:`, `fix:`, `chore:`, `refactor:`, `docs:`, `test:`, `perf:`,
  `style:`, `build:`, `ci:`). No scope in parentheses.
- No `Co-Authored-By` trailers, no AI tool attributions in commits, PR
  descriptions, or review comments.
- Add or update tests when you change the gateway. The test fixtures in
  `internal/gateway/*_test.go` and `internal/store/*_test.go` show the
  shape.
- Do not commit secrets, real provider keys, the contents of
  `~/.intellyrouter/master.key`, or anything under `bin/`. The `.gitignore`
  covers the common cases.

## Coding style

- Go: `gofmt`, `go vet ./...` clean. The existing files set the bar for
  naming and density.
- TypeScript / React: `eslint .` clean. Match the surrounding file's
  imports and component shape.
- Comments only for non-obvious whys, in a single line, in the same
  style as the file. The commit is the what; the code is the what
  the next person will see; a comment is a *why* the next person
  cannot see.

## Releases

The maintainers cut releases from `main`. A release is a signed tag
(`git tag -s v0.x.y -m '...'`) followed by `git push origin v0.x.y`.
The release workflow builds binaries and drafts the GitHub Release.
