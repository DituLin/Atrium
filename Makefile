# Atrium Home Hub — build and check targets.

BIN_DIR    := bin
BINARY     := $(BIN_DIR)/atrium
PKG        := ./...
VERSION    ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0)
COMMIT     ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -X github.com/DituLin/Atrium/internal/version.Version=$(VERSION) \
              -X github.com/DituLin/Atrium/internal/version.Commit=$(COMMIT) \
              -X github.com/DituLin/Atrium/internal/version.BuildDate=$(BUILD_DATE)

DEV_CONFIG ?= config.yaml

.PHONY: all build web test lint check run-dev release e2e clean fmt tidy

all: check

build:
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags '$(LDFLAGS)' -o $(BINARY) ./cmd/atrium

web:
	@if [ -f web/package.json ]; then \
		cd web && npm ci --no-audit --no-fund && npm run build && cd .. && $(MAKE) web-sync; \
	else \
		echo "web/package.json missing; skipping web build"; \
	fi

test:
	go test $(PKG)

lint:
	go vet $(PKG)
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run; \
	else \
		echo "golangci-lint not installed; run 'brew install golangci-lint'"; exit 1; \
	fi

check: lint test build

run-dev: build
	$(BINARY) serve --config $(DEV_CONFIG) --dev

release: web-sync
	@mkdir -p $(BIN_DIR)
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/atrium-darwin-arm64 ./cmd/atrium
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN_DIR)/atrium-darwin-amd64 ./cmd/atrium

e2e:
	@if [ -d scripts/e2e/node_modules/playwright ]; then \
		cd scripts/e2e && npm run smoke; \
	else \
		echo "e2e: install Playwright first: (cd scripts/e2e && npm install); then start a tls-off server and run ATRIUM_CFG=<config> make e2e (see scripts/e2e/README.md)"; \
	fi

fmt:
	gofmt -w $(shell git ls-files '*.go' 2>/dev/null || find . -name '*.go' -not -path './web/node_modules/*')

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)

## web-sync: copy the Vite bundle into the Go embed directory
web-sync:
	@rm -rf internal/webui/dist && mkdir -p internal/webui/dist
	@if [ -f web/dist/index.html ]; then cp -R web/dist/. internal/webui/dist/; fi
	@touch internal/webui/dist/.gitkeep
