# Copyright 2026 Bret Jordan, All rights reserved.
#
# Use of this source code is governed by an Apache 2.0 license
# that can be found in the LICENSE file in the root of the source tree.
#
# This Makefile builds, tests, and packages RouterCLI, the shipped example
# CLI that demonstrates the framework, and the framework itself, since both
# live in the same module. Every check this file runs locally also runs in
# .github/workflows/ci.yml on every push and pull request, so "make ci"
# gives a contributor the same result before ever pushing a commit.

GO_CMD     := go
GO_BUILD   := $(GO_CMD) build
GO_CLEAN   := $(GO_CMD) clean
GO_TEST    := $(GO_CMD) test
GO_VET     := $(GO_CMD) vet
GO_INSTALL := $(GO_CMD) install -v
GOFMT      := gofmt

MODULE := github.com/gotermcli/routercli

# BINARY is the CLI client. DAEMON_BINARY is the multiuser server that lets
# every attached session share one running configuration.
BINARY        := routercli
DAEMON_BINARY := routerclid
# The library is the module root. The worked example, and both binaries
# built from it, live under example/.
EXAMPLE_PKG   := ./example
DAEMON_PKG    := ./example/cmd/multiuser

# VERSION is bumped by hand for each release. BUILD is read from the current
# commit, so a development build always carries a real, traceable commit hash
# without anyone needing to update it.
VERSION := 0.1.0
BUILD   := $(shell git rev-parse HEAD 2>/dev/null || echo "unknown")

BIN_DIR        := bin
DIST_DIR       := dist
COVERAGE_FILE  := coverage.txt

LDFLAGS := -X main.Version=$(VERSION) -X main.Build=$(BUILD)

NO_COLOR    = \033[0m
OK_COLOR    = \033[32;01m
ERROR_COLOR = \033[31;01m
WARN_COLOR  = \033[33;01m

.DEFAULT_GOAL := help

.PHONY: all help build build-client build-daemon clean test race coverage coverage-html vet fmt \
        fmt-check lint ci check-config run install uninstall deps tidy distro

all: build ## Alias for build.

help: ## List every target in this Makefile with a short description.
	@grep -E '^[a-zA-Z_.-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "$(OK_COLOR)%-16s$(NO_COLOR) %s\n", $$1, $$2}'

build: build-client build-daemon ## Build both binaries into bin/.

build-client: ## Build the routercli client binary into bin/.
	@echo "$(OK_COLOR)==> Building $(BINARY) $(VERSION) ($(BUILD))...$(NO_COLOR)"
	@mkdir -p $(BIN_DIR)
	$(GO_BUILD) -v -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(BINARY) $(EXAMPLE_PKG)

build-daemon: ## Build the routerclid multiuser daemon binary into bin/.
	@echo "$(OK_COLOR)==> Building $(DAEMON_BINARY) $(VERSION) ($(BUILD))...$(NO_COLOR)"
	@mkdir -p $(BIN_DIR)
	$(GO_BUILD) -v -ldflags "$(LDFLAGS)" -o $(BIN_DIR)/$(DAEMON_BINARY) $(DAEMON_PKG)

clean: ## Remove every build, coverage, and distribution artifact.
	@echo "$(OK_COLOR)==> Removing Build And Distribution Artifacts...$(NO_COLOR)"
	@$(GO_CLEAN)
	@rm -rf $(BIN_DIR) $(DIST_DIR) $(COVERAGE_FILE)

test: ## Run the full test suite.
	@echo "$(OK_COLOR)==> Running Test Suite...$(NO_COLOR)"
	$(GO_TEST) ./...

race: ## Run the full test suite with the race detector enabled.
	@echo "$(OK_COLOR)==> Running Test Suite With The Race Detector...$(NO_COLOR)"
	$(GO_TEST) -race ./...

coverage: ## Run the test suite and print a per-function coverage report.
	@echo "$(OK_COLOR)==> Running Test Suite With Coverage...$(NO_COLOR)"
	$(GO_TEST) -coverprofile=$(COVERAGE_FILE) ./...
	$(GO_CMD) tool cover -func=$(COVERAGE_FILE)

coverage-html: coverage ## Run the test suite and open an HTML coverage report.
	@echo "$(OK_COLOR)==> Opening Coverage Report In A Browser...$(NO_COLOR)"
	$(GO_CMD) tool cover -html=$(COVERAGE_FILE)

vet: ## Run go vet across every package.
	@echo "$(OK_COLOR)==> Running Go Vet...$(NO_COLOR)"
	$(GO_VET) ./...

fmt: ## Reformat every source file with gofmt.
	@echo "$(OK_COLOR)==> Formatting Source Files...$(NO_COLOR)"
	$(GOFMT) -l -w .

fmt-check: ## Fail if any source file is not gofmt formatted, without changing anything.
	@echo "$(OK_COLOR)==> Checking Source File Formatting...$(NO_COLOR)"
	@files=`$(GOFMT) -l .`; \
	if [ -n "$$files" ]; then \
		echo "$(ERROR_COLOR)==> The following files need gofmt:$(NO_COLOR)"; \
		echo "$$files"; \
		exit 1; \
	fi

lint: ## Run golangci-lint if it is installed, and skip with a notice otherwise.
	@if command -v golangci-lint >/dev/null 2>&1; then \
		echo "$(OK_COLOR)==> Running golangci-lint...$(NO_COLOR)"; \
		golangci-lint run ./...; \
	else \
		echo "$(WARN_COLOR)==> golangci-lint is not installed, skipping. See https://golangci-lint.run/welcome/install/$(NO_COLOR)"; \
	fi

ci: fmt-check vet test race build ## Run every check the GitHub Actions workflow runs, in the same order.
	@echo "$(OK_COLOR)==> All CI Checks Passed...$(NO_COLOR)"

check-config: build ## Build, then verify the shipped example configuration loads and every Command Level resolves.
	@echo "$(OK_COLOR)==> Verifying The Shipped Example Configuration...$(NO_COLOR)"
	./$(BIN_DIR)/$(BINARY) --config example/etc/routercli.yaml --check-config

run: build ## Build, then run the shipped example CLI against its own configuration.
	@echo "$(OK_COLOR)==> Running $(BINARY)...$(NO_COLOR)"
	./$(BIN_DIR)/$(BINARY) --config example/etc/routercli.yaml

deps: ## Download and verify every module dependency.
	@echo "$(OK_COLOR)==> Downloading Dependencies...$(NO_COLOR)"
	$(GO_CMD) mod download
	$(GO_CMD) mod verify

tidy: ## Tidy go.mod and go.sum to match the current source tree.
	@echo "$(OK_COLOR)==> Tidying go.mod And go.sum...$(NO_COLOR)"
	$(GO_CMD) mod tidy

install: ## Install both binaries into GOPATH/bin.
	@echo "$(OK_COLOR)==> Installing $(BINARY) And $(DAEMON_BINARY) $(VERSION) ($(BUILD))...$(NO_COLOR)"
	$(GO_INSTALL) -ldflags "$(LDFLAGS)" $(EXAMPLE_PKG)
	$(GO_BUILD) -ldflags "$(LDFLAGS)" -o `$(GO_CMD) env GOPATH`/bin/$(DAEMON_BINARY) $(DAEMON_PKG)

uninstall: ## Remove both binaries from GOPATH/bin.
	@echo "$(OK_COLOR)==> Removing Installed $(BINARY) And $(DAEMON_BINARY)...$(NO_COLOR)"
	@rm -f `$(GO_CMD) env GOPATH`/bin/$(BINARY)
	@rm -f `$(GO_CMD) env GOPATH`/bin/$(DAEMON_BINARY)

distro: clean build ## Build a distributable tarball with the binary, etc/, var/, README.md, and LICENSE.
	@echo "$(OK_COLOR)==> Setting Up Distribution Directory...$(NO_COLOR)"
	@mkdir -p $(DIST_DIR)/$(BINARY)-$(VERSION)/etc
	@mkdir -p $(DIST_DIR)/$(BINARY)-$(VERSION)/var
	@echo "$(OK_COLOR)==> Copying Application Files...$(NO_COLOR)"
	@cp $(BIN_DIR)/$(BINARY) $(DIST_DIR)/$(BINARY)-$(VERSION)/
	@cp $(BIN_DIR)/$(DAEMON_BINARY) $(DIST_DIR)/$(BINARY)-$(VERSION)/
	@cp -R etc/. $(DIST_DIR)/$(BINARY)-$(VERSION)/etc/
	@cp -R var/. $(DIST_DIR)/$(BINARY)-$(VERSION)/var/
	@cp README.md $(DIST_DIR)/$(BINARY)-$(VERSION)/
	@cp LICENSE $(DIST_DIR)/$(BINARY)-$(VERSION)/
	@echo "$(OK_COLOR)==> Creating Tarball...$(NO_COLOR)"
	@cd $(DIST_DIR) && tar -czf $(BINARY)-$(VERSION).tar.gz $(BINARY)-$(VERSION)
	@echo "$(OK_COLOR)==> Distribution Package Ready At $(DIST_DIR)/$(BINARY)-$(VERSION).tar.gz$(NO_COLOR)"
