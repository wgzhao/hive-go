SHELL := /bin/bash
export GOTOOLCHAIN := local

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
BIN := hive
LDFLAGS := -s -w -X main.version=$(VERSION)
GO ?= go

.PHONY: all build install vet e2e test generate clean release cross help

all: build

build:
	CGO_ENABLED=0 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/hive

install:
	CGO_ENABLED=0 $(GO) install -trimpath -ldflags "$(LDFLAGS)" ./cmd/hive

vet:
	$(GO) vet ./...

e2e: build
	bash cmd/hive/e2e/run.sh

# There are deliberately no unit tests; E2E is the verification mechanism.
test: vet

generate:
	@thrift --version | grep -q '0.19.0' || { echo "thrift 0.19.0 is required (found: $$(thrift --version 2>&1))" >&2; exit 1; }
	rm -f internal/tcliservice/TCLIService.go internal/tcliservice/TCLIService-consts.go internal/tcliservice/GoUnusedProtection__.go
	thrift --gen go -out internal/tcliservice internal/tcliservice/TCLIService.thrift
	mv internal/tcliservice/tcliservice/*.go internal/tcliservice/
	rm -rf internal/tcliservice/tcliservice
	gofmt -w internal/tcliservice/*.go

clean:
	rm -f $(BIN)
	rm -rf dist

release: cross
	@echo "release binaries are under dist/"

cross:
	mkdir -p dist
	@set -e; for os in linux darwin windows; do \
		for arch in amd64 arm64; do \
			ext=""; [ "$$os" = "windows" ] && ext=".exe"; \
			out="dist/$(BIN)-$(VERSION)-$$os-$$arch$$ext"; \
			echo "building $$out"; \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o "$$out" ./cmd/hive; \
		done; \
	done

help:
	@echo "Targets:"
	@echo "  build      build ./$(BIN) (the installed command is `hive`)"
	@echo "  install    go install the CLI as `hive`"
	@echo "  vet        go vet ./..."
	@echo "  e2e        run the real-server E2E suite (requires HIVE_ALLOW_DDL=1)"
	@echo "  test       alias for vet (no unit tests)"
	@echo "  generate   regenerate internal/tcliservice (thrift 0.19.0)"
	@echo "  cross      cross-compile linux/darwin/windows amd64+arm64"
	@echo "  release    same as cross, binaries in dist/"
	@echo "  clean      remove the binary and dist/"
