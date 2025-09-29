# Makefile for deps - wraps Taskfile.yml targets
# Automatically installs task if not present

SHELL := /bin/bash
TASK_VERSION := v3.39.2
TASK_BIN := ./bin/task
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

# Detect platform
ifeq ($(UNAME_S),Darwin)
	TASK_PLATFORM := darwin
else ifeq ($(UNAME_S),Linux)
	TASK_PLATFORM := linux
else
	TASK_PLATFORM := windows
endif

# Detect architecture
ifeq ($(UNAME_M),x86_64)
	TASK_ARCH := amd64
else ifeq ($(UNAME_M),arm64)
	TASK_ARCH := arm64
else ifeq ($(UNAME_M),aarch64)
	TASK_ARCH := arm64
else
	TASK_ARCH := 386
endif

# Task binary URL
TASK_URL := https://github.com/go-task/task/releases/download/$(TASK_VERSION)/task_$(TASK_PLATFORM)_$(TASK_ARCH).tar.gz

# Ensure task is installed
$(TASK_BIN):
	@echo "Installing task $(TASK_VERSION) to $(TASK_BIN)..."
	@mkdir -p ./bin
	@curl -sL $(TASK_URL) | tar -xz -C ./bin task
	@chmod +x $(TASK_BIN)
	@echo "Task installed successfully"

# Default target - list all available tasks
.PHONY: default
default: $(TASK_BIN)
	@$(TASK_BIN) --list

.PHONY: help
help: default

# Build targets
.PHONY: build
build: $(TASK_BIN)
	@$(TASK_BIN) build

.PHONY: build-all
build-all: $(TASK_BIN)
	@$(TASK_BIN) build-all

# Test targets
.PHONY: test
test: $(TASK_BIN)
	@$(TASK_BIN) test

.PHONY: test-report
test-report: $(TASK_BIN)
	@$(TASK_BIN) test:report

.PHONY: test-e2e-report
test-e2e-report: $(TASK_BIN)
	@$(TASK_BIN) test:e2e-report

.PHONY: test-failed
test-failed: $(TASK_BIN)
	@$(TASK_BIN) test:failed

.PHONY: test-parse-failed
test-parse-failed: $(TASK_BIN)
	@$(TASK_BIN) test:parse-failed

# Code quality targets
.PHONY: lint
lint: $(TASK_BIN)
	@$(TASK_BIN) lint

.PHONY: fmt
fmt: $(TASK_BIN)
	@$(TASK_BIN) fmt

.PHONY: vet
vet: $(TASK_BIN)
	@$(TASK_BIN) vet

# Module management
.PHONY: mod-tidy
mod-tidy: $(TASK_BIN)
	@$(TASK_BIN) mod-tidy

.PHONY: mod-download
mod-download: $(TASK_BIN)
	@$(TASK_BIN) mod-download

# Cleanup
.PHONY: clean
clean: $(TASK_BIN)
	@$(TASK_BIN) clean

.PHONY: clean-all
clean-all: clean
	@rm -rf $(TASK_BIN)

# Install
.PHONY: install
install: $(TASK_BIN)
	@$(TASK_BIN) install

# Aggregate targets
.PHONY: check
check: $(TASK_BIN)
	@$(TASK_BIN) check

.PHONY: ci
ci: $(TASK_BIN)
	@$(TASK_BIN) ci

# Show task version
.PHONY: task-version
task-version: $(TASK_BIN)
	@$(TASK_BIN) --version