.PHONY: build web test lint

build: web
	go build -tags webui -o bin/intellyrouter ./cmd/intellyrouter

web:
	cd web && bun install --frozen-lockfile && bun run build

test:
	go test -race ./...
	cd web && bun run typecheck && bun run lint

lint:
	golangci-lint run ./...
