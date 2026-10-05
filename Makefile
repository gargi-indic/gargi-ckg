# Makefile for gargi-ckg

GOLANGCI_LINT_VERSION := v1.64.6
COMPOSE_FILE ?= docker-compose.yml

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
	@if [ ! -f ./bin/golangci-lint ]; then \
		curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b ./bin $(GOLANGCI_LINT_VERSION); \
	fi

build:
	go build ./...

test:
	go test -race -count=1 ./...

test-integration:
	go test -race -count=1 -tags=integration ./...

lint: tools
	./bin/golangci-lint run

fmt:
	go fmt ./...
	go run mvdan.cc/gofumpt@latest -l -w .

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
