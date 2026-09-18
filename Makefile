SHELL := /bin/sh

.DEFAULT_GOAL := build

GO ?= go
GOFLAGS ?=
APP_NAME ?= easycode
CMD_PATH := ./cmd/easycode
BIN_DIR ?= bin
BIN_PATH := $(BIN_DIR)/$(APP_NAME)
PACKAGES := ./...

.PHONY: build clean fmt-check vet lint test test-race verify install-hooks

build:
	@mkdir -p "$(BIN_DIR)"
	$(GO) build $(GOFLAGS) -trimpath -o "$(BIN_PATH)" "$(CMD_PATH)"

fmt-check:
	@files="$$(gofmt -l .)"; \
	if [ -n "$$files" ]; then \
		printf '%s\n' 'Go files are not formatted:'; \
		printf '%s\n' "$$files"; \
		exit 1; \
	fi

vet:
	$(GO) vet $(PACKAGES)

lint:
	$(GO) tool staticcheck $(PACKAGES)

test:
	$(GO) test $(PACKAGES)

test-race:
	$(GO) test -race $(PACKAGES)

verify: fmt-check vet lint test test-race

install-hooks:
	@if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then \
		printf '%s\n' 'error: initialize a Git repository before installing hooks' >&2; \
		exit 1; \
	fi
	@git config core.hooksPath .githooks
	@chmod +x .githooks/pre-commit
	@printf '%s\n' 'Git hooks installed from .githooks.'

clean:
	rm -f "$(BIN_PATH)"
