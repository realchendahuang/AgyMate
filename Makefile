BINARY         := agymate
DESKTOP_BINARY := agymate-desktop
VERSION        ?= 0.1.0
LDFLAGS        := -s -w -X github.com/realchendahuang/AgyMate/internal/version.Version=$(VERSION)
UNAME_S        := $(shell uname -s)

ifeq ($(UNAME_S),Darwin)
DESKTOP_CGO_ENV := CGO_ENABLED=1 CGO_CFLAGS="-mmacosx-version-min=12.0" CGO_LDFLAGS="-mmacosx-version-min=12.0" MACOSX_DEPLOYMENT_TARGET=12.0
else
DESKTOP_CGO_ENV := CGO_ENABLED=1
endif

.PHONY: all build desktop run run-desktop serve test test-race vet fmt tidy check verify install clean

all: build

build:
	@mkdir -p bin
	CGO_ENABLED=1 go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/agymate

desktop:
	@mkdir -p bin
	$(DESKTOP_CGO_ENV) go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(DESKTOP_BINARY) ./cmd/agymate-desktop

run-desktop: desktop
	./bin/$(DESKTOP_BINARY)

serve: build
	./bin/$(BINARY) serve

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

verify: vet test-race

check: verify build desktop

install:
	VERSION="$(VERSION)" ./scripts/install.sh

clean:
	rm -rf bin dist
