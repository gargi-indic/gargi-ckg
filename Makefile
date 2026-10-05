# Makefile for gargi-ckg

GOLANGCI_LINT_VERSION := v2.14.0
COMPOSE_FILE ?= docker-compose.yml
GOLANGCI_LINT_BIN := ./bin/golangci-lint-$(GOLANGCI_LINT_VERSION)

.PHONY: help tools build test test-integration lint fmt generate up down

help:
	@echo "Usage: make [target]"
	@echo ""
	@echo "Targets:"
	@echo "  help             Show this help message"
	@echo "  tools            Install necessary development tools"
	@echo "  build            Build the project"
	@echo "  test             Run unit tests"
	@echo "  test-integration Run integration tests"
	@echo "  lint             Run linters"
	@echo "  fmt              Format the code"
	@echo "  generate         Run code generation"
	@echo "  up               Start services (requires docker-compose.yml)"
	@echo "  down             Stop services"

tools:
	@mkdir -p ./bin
	@if [ ! -f $(GOLANGCI_LINT_BIN) ]; then \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/$(GOLANGCI_LINT_VERSION)/install.sh | sh -s -- -b ./bin $(GOLANGCI_LINT_VERSION) && \
		mv ./bin/golangci-lint $(GOLANGCI_LINT_BIN); \
	fi

build:
	go build ./...

test:
	go test -race -count=1 ./...

test-integration:
	go test -race -count=1 -tags=integration ./...

lint: tools
	$(GOLANGCI_LINT_BIN) run

fmt: tools
	$(GOLANGCI_LINT_BIN) fmt

generate:
	go generate ./...

up:
	@if [ ! -f $(COMPOSE_FILE) ]; then \
		echo "arrives with #4"; \
		false; \
	else \
		docker compose -f $(COMPOSE_FILE) up -d; \
	fi

down:
	@if [ ! -f $(COMPOSE_FILE) ]; then \
		echo "arrives with #4"; \
		false; \
	else \
		docker compose -f $(COMPOSE_FILE) down; \
	fi
