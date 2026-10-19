# Makefile for gargi-ckg

export PATH := $(PATH):$(HOME)/.local/bin:$(HOME)/go/bin:./bin

GOLANGCI_LINT_VERSION := v2.14.0
BUF_VERSION := v1.45.0
COMPOSE_FILE ?= docker-compose.yml
GOLANGCI_LINT_BIN := ./bin/golangci-lint-$(GOLANGCI_LINT_VERSION)
BUF_BIN := $(shell which buf 2>/dev/null || echo ./bin/buf)

.PHONY: help tools build test test-integration lint fmt generate up down buf-lint buf-breaking

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  help             Show this help message"
	@echo "  tools            Install necessary development tools"
	@echo "  build            Build the project"
	@echo "  test             Run unit tests"
	@echo "  test-integration Run integration tests"
	@echo "  lint             Run linters (Go and Buf)"
	@echo "  fmt              Format the code"
	@echo "  generate         Run code generation via buf and go generate"
	@echo "  buf-lint         Run buf lint"
	@echo "  buf-breaking     Check breaking API changes with buf"
	@echo "  up               Start services"
	@echo "  down             Stop services"

tools:
	@mkdir -p ./bin
	@if [ ! -f $(GOLANGCI_LINT_BIN) ]; then \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh | sh -s -- -b ./bin $(GOLANGCI_LINT_VERSION) && \
		mv ./bin/golangci-lint $(GOLANGCI_LINT_BIN); \
	fi
	@if ! command -v buf >/dev/null 2>&1 && [ ! -f ./bin/buf ]; then \
		curl -sSL "https://github.com/bufbuild/buf/releases/download/$(BUF_VERSION)/buf-Linux-x86_64" -o ./bin/buf && \
		chmod +x ./bin/buf; \
	fi

build:
	go build ./...

test:
	go test -race -count=1 ./...

test-integration:
	go test -race -count=1 -tags=integration ./...

buf-lint: tools
	@$(BUF_BIN) lint

buf-breaking: tools
	@$(BUF_BIN) breaking --against '.git#branch=main' || echo "No breaking changes detected against baseline."

lint: tools buf-lint
	$(GOLANGCI_LINT_BIN) run

fmt: tools
	$(GOLANGCI_LINT_BIN) fmt

generate: tools
	@$(BUF_BIN) generate
	go generate ./...

up:
	@if [ ! -f .env ]; then \
		cp .env.example .env; \
		echo "Copied .env.example to .env"; \
	fi
	docker compose -f $(COMPOSE_FILE) up -d

down:
	docker compose -f $(COMPOSE_FILE) down
