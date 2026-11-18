VERSION ?= 0.1.1
BUILD_ID ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo dev)
GOCACHE ?= /tmp/agy-statusline-go-cache
TARGET_DIR ?= $(HOME)/.local/bin
LDFLAGS := -s -w -X main.version=$(VERSION) -X main.buildID=$(BUILD_ID)

.PHONY: help build install enable test race vet benchmark audit verify release-assets

help:
	@echo "Available targets:"
	@echo "  build           Build agy-statusline binary in the repository root"
	@echo "  install         Build and install agy-statusline to $(TARGET_DIR)"
	@echo "  enable          Install binary and register statusLine in Antigravity CLI settings.json"
	@echo "  test            Run unit tests"
	@echo "  race            Run unit tests with the Go race detector"
	@echo "  vet             Run go vet"
	@echo "  benchmark       Run rendering benchmarks"
	@echo "  audit           Check formatting and verify clean open-source tree (no AI dirs, .github, or PII)"
	@echo "  verify          Run audit, vet, race tests, and benchmarks"
	@echo "  release-assets  Cross-compile archives and SHA256SUMS into dist/release/"

build:
	GOCACHE=$(GOCACHE) go build -trimpath -ldflags "$(LDFLAGS)" -o agy-statusline ./cmd/agy-statusline

install: build
	@mkdir -p "$(TARGET_DIR)"
	@set -e; tmp="$(TARGET_DIR)/.agy-statusline.tmp.$$$$"; cp -f ./agy-statusline "$$tmp"; chmod 755 "$$tmp"; mv -f "$$tmp" "$(TARGET_DIR)/agy-statusline"
	@rm -f ./agy-statusline
	@"$(TARGET_DIR)/agy-statusline" version

enable: install
	"$(TARGET_DIR)/agy-statusline" install

test:
	GOCACHE=$(GOCACHE) go test ./...

race:
	GOCACHE=$(GOCACHE) go test -race ./...

vet:
	GOCACHE=$(GOCACHE) go vet ./...

benchmark:
	GOCACHE=$(GOCACHE) go test -run=^$$ -bench=. ./internal/hud

audit:
	@unformatted=$$(gofmt -l cmd internal); \
	if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi
	@for p in .github .claude .codex .gemini .grok .agents .agent .serena .superpowers .cursor .windsurf graft .graft AGENTS.md CLAUDE.md GEMINI.md GROK.md .mcp.json opencode.json; do \
		if [ -e "$$p" ]; then echo "forbidden file/dir present: $$p"; exit 1; fi; \
	done
	@if grep -rnE "(@[g]mail\.com|/Volumes/)" cmd internal docs README.md CHANGELOG.md CONTRIBUTING.md SECURITY.md install.sh install.ps1 scripts >/dev/null 2>&1; then \
		echo "personal email or machine path found in tracked source"; \
		exit 1; \
	fi
	@echo "✓ audit clean"

verify: audit vet race benchmark

release-assets:
	VERSION=$(VERSION) BUILD_ID=$(BUILD_ID) ./scripts/build-release.sh $(VERSION) $(BUILD_ID) dist/release
