.PHONY: dev test web build linux

dev:
	go run ./cmd/server

test:
	go test -race ./...

web:
	cd web && pnpm install --frozen-lockfile && pnpm build

build: web
	mkdir -p bin
	CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/work-schedule ./cmd/server

linux: web
	mkdir -p bin
	GOOS=linux GOARCH=$${GOARCH:-amd64} CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o bin/work-schedule ./cmd/server
