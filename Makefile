SHELL := /bin/sh

.DEFAULT_GOAL := build

GO ?= go
GOFLAGS ?=
PYTHON ?= python3
# 默认不限制总耗时；保留每测试进程超时和可选诊断预算。
TEST_TIMEOUT ?= 20m
TEST_BUDGET ?= 0
TEST_OPTIONS ?=
SMOKE_OPTIONS ?=
TEST_RUNNER := $(PYTHON) -B scripts/test_suite.py --go "$(GO)" --timeout=$(TEST_TIMEOUT) --budget=$(TEST_BUDGET) $(TEST_OPTIONS)
APP_NAME ?= easycode
CMD_PATH := ./cmd/easycode
BIN_DIR ?= bin
BIN_PATH := $(BIN_DIR)/$(APP_NAME)
PACKAGES := ./...

.PHONY: build clean branch-check branch-policy-test fmt-check vet lint test test-race test-race-full test-all test-ratio test-runner-test smoke verify install-hooks

# 同一 make 调用中的普通/race 入口串行；各阶段由 runner 统一分配有限进程。
.NOTPARALLEL:

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
	$(TEST_RUNNER) --mode=normal

test-race:
	$(TEST_RUNNER) --mode=race

test-race-full:
	$(TEST_RUNNER) --mode=race --race-scope=full

test-all:
	$(TEST_RUNNER) --mode=all

test-runner-test:
	$(PYTHON) -B -m unittest discover -s scripts -p '*_test.py'

test-ratio:
	$(PYTHON) -B scripts/test_ratio.py

smoke:
	$(PYTHON) -B scripts/smoke_suite.py $(SMOKE_OPTIONS)

branch-check:
	@./scripts/check-branch-name.sh

branch-policy-test:
	@./scripts/check-branch-name_test.sh

verify: test-ratio
	$(TEST_RUNNER) --mode=verify

install-hooks:
	@if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then \
		printf '%s\n' 'error: initialize a Git repository before installing hooks' >&2; \
		exit 1; \
	fi
	@git config core.hooksPath .githooks
	@chmod +x .githooks/pre-commit scripts/check-branch-name.sh scripts/check-branch-name_test.sh
	@printf '%s\n' 'Git hooks installed from .githooks.'

clean:
	rm -f "$(BIN_PATH)"
