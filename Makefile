GOLANGCI_LINT = go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2
GOVULNCHECK = go run golang.org/x/vuln/cmd/govulncheck@latest

.PHONY: build web test lint vuln

build: web
	go build -tags webui -o bin/intellyrouter ./cmd/intellyrouter

web:
	cd web && bun install --frozen-lockfile && bun run build

test:
	go test -race ./...
	cd web && bun run typecheck && bun run lint

lint:
	$(GOLANGCI_LINT) run ./...

vuln:
	$(GOVULNCHECK) ./...
	cd web && bun audit
