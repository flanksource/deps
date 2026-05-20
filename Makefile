# Makefile for deps - wraps Taskfile.yml targets
# Automatically installs task if not present

SHELL := /bin/bash
NAME := deps
DATE := $(shell date "+%Y-%m-%d %H:%M:%S")
ifeq ($(VERSION),)
VERSION_TAG := $(shell git describe --abbrev=0 --tags --exact-match 2>/dev/null || echo latest)
else
VERSION_TAG := $(VERSION)
endif

TASK_VERSION := v3.39.2
UPX_VERSION := 3.96
TASK_BIN := ./bin/task
UPX := ./.bin/upx
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
SHA256 := $(shell command -v sha256sum >/dev/null 2>&1 && echo "sha256sum" || echo "shasum -a 256")

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

.PHONY: linux
linux:
	mkdir -p .bin
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ./.bin/$(NAME)-linux-amd64 -ldflags "-X main.version=$(VERSION_TAG)" ./cmd/deps/main.go
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o ./.bin/$(NAME)-linux-arm64 -ldflags "-X main.version=$(VERSION_TAG)" ./cmd/deps/main.go

.PHONY: darwin
darwin:
	mkdir -p .bin
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o ./.bin/$(NAME)-darwin-amd64 -ldflags "-X main.version=$(VERSION_TAG)" ./cmd/deps/main.go
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -o ./.bin/$(NAME)-darwin-arm64 -ldflags "-X main.version=$(VERSION_TAG)" ./cmd/deps/main.go

.PHONY: windows
windows:
	mkdir -p .bin
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o ./.bin/$(NAME)-windows-amd64.exe -ldflags "-X main.version=$(VERSION_TAG)" ./cmd/deps/main.go

.PHONY: compress
compress:
ifeq ($(TASK_PLATFORM),linux)
	$(MAKE) $(UPX)
	$(UPX) -5 ./.bin/$(NAME)-linux-amd64 ./.bin/$(NAME)-linux-arm64
else
	@echo "Skipping upx compression on $(TASK_PLATFORM)"
endif

.PHONY: binaries
binaries: linux darwin windows compress

$(UPX): .bin
	wget -nv -O upx.tar.xz https://github.com/upx/upx/releases/download/v$(UPX_VERSION)/upx-$(UPX_VERSION)-$(TASK_ARCH)_$(TASK_PLATFORM).tar.xz
	tar xf upx.tar.xz
	mv upx-$(UPX_VERSION)-$(TASK_ARCH)_$(TASK_PLATFORM)/upx .bin
	rm -rf upx.tar.xz upx-$(UPX_VERSION)-$(TASK_ARCH)_$(TASK_PLATFORM)

.bin:
	mkdir -p .bin

.PHONY: release
release: binaries
	mkdir -p .release
	rm -f .release/deps-* .release/deps .release/deps.exe
	@for binary in .bin/$(NAME)-*; do \
		artifact=$$(basename "$$binary"); \
		archive_base="$${artifact%.exe}"; \
		(cd .bin && $(SHA256) "$$artifact") > ".release/$$artifact.sha256"; \
		if [[ "$$artifact" == *.exe ]]; then \
			cp "$$binary" ".release/$$artifact"; \
			cp "$$binary" .release/$(NAME).exe; \
			(cd .release && zip -q "$$archive_base.zip" $(NAME).exe && $(SHA256) "$$archive_base.zip" > "$$archive_base.sha256"); \
			rm -f .release/$(NAME).exe; \
		else \
			cp "$$binary" .release/$(NAME); \
			(cd .release && tar czf "$$archive_base.tar.gz" $(NAME) && $(SHA256) "$$archive_base.tar.gz" > "$$archive_base.tar.gz.sha256"); \
			rm -f .release/$(NAME); \
		fi; \
	done

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