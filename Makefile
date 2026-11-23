BINARY     := agymate
VERSION    ?= 0.4.0
GIT_COMMIT := $(shell git describe --tags --always --dirty 2>/dev/null || git rev-parse --short HEAD 2>/dev/null || echo dev)
BUILD_TIME := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -s -w -X github.com/realchendahuang/AgyMate/internal/version.Version=$(VERSION) -X github.com/realchendahuang/AgyMate/internal/version.Commit=$(GIT_COMMIT) -X github.com/realchendahuang/AgyMate/internal/version.BuildTime=$(BUILD_TIME)
JS_FILES   := frontend/i18n.js frontend/app.js internal/cdp/hud.js internal/cdp/zh_inject.js magpie-plugin/page/boot.js magpie-plugin/index.mjs mcpapp-ui/src/main.js mcpapp-ui/build.mjs
BUILD_ENV  := CGO_ENABLED=0

.PHONY: all build build-mcp-app ensure-mcp-app cross-check run serve test test-race vet fmt tidy js-check check verify install install-dev clean

all: build

build-mcp-app:
	@if command -v bun >/dev/null 2>&1; then \
		cd mcpapp-ui && ([ -d node_modules ] || bun install) && bun run build; \
	elif [ -f internal/mcpapp/dist/app.html ]; then \
		echo "bun not found, using prebuilt internal/mcpapp/dist/app.html"; \
	else \
		echo "error: bun not found and internal/mcpapp/dist/app.html missing" >&2; exit 1; \
	fi

ensure-mcp-app:
	@if [ ! -f internal/mcpapp/dist/app.html ]; then \
		$(MAKE) build-mcp-app; \
	fi

build: ensure-mcp-app
	@mkdir -p bin
	$(BUILD_ENV) go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/agymate

cross-check: ensure-mcp-app
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o /dev/null ./cmd/agymate
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -o /dev/null ./cmd/agymate
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -o /dev/null ./cmd/agymate
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /dev/null ./cmd/agymate

serve: build
	./bin/$(BINARY) serve

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

js-check:
	@if command -v node >/dev/null 2>&1; then \
		for f in $(JS_FILES); do node --check "$$f"; done; \
	else \
		echo "node not found, skipping JS syntax check"; \
	fi

fmt:
	gofmt -w .

tidy:
	go mod tidy

verify: vet test-race js-check

check: verify build

install:
	VERSION="$(VERSION)" ./scripts/install.sh

install-dev:
	VERSION="$(VERSION)" MAGPIE_PLUGIN_DEV=1 ./scripts/install.sh

clean:
	rm -rf bin dist
