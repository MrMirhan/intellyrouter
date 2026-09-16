## Summary

One paragraph on what this PR does and why. Link the issue with
`Closes #N` if it closes one.

## Test plan

How you verified it works. Include the output of `go test ./...` and
`cd web && bun run build` for code changes that touch either side.

## Checklist

- [ ] One commit per logical change.
- [ ] Commit messages use a semantic type only (`feat:`, `fix:`,
      `chore:`, `refactor:`, `docs:`, `test:`, `perf:`, `style:`,
      `build:`, `ci:`). No scope in parentheses.
- [ ] No `Co-Authored-By` or AI-tool trailers.
- [ ] `go vet ./...` clean.
- [ ] `go test ./...` clean.
- [ ] `cd web && bunx tsc --noEmit -p tsconfig.app.json && bun run lint && bun run build` clean.
- [ ] No secrets in the diff (`live-key.json`, `master.key`, `*.db`,
      `sk-*`, `ghp_*`).
