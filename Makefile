.DEFAULT_GOAL := run

ROOT := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
TOOLS := $(ROOT)/.tools
GOROOT := $(TOOLS)/go
GO := $(GOROOT)/bin/go
GO_VERSION := 1.27.2

export PATH := $(GOROOT)/bin:$(PATH)
export GOROOT

HTTP_ADDR ?= :8080

.PHONY: run build test install-go

run: install-go
	@set -a; [ -f .env ] && . ./.env; set +a; \
	HTTP_ADDR="$(HTTP_ADDR)" "$(GO)" run ./cmd/api

build: install-go
	mkdir -p bin
	"$(GO)" build -o bin/halfpos-api ./cmd/api

test: install-go
	"$(GO)" test ./...

install-go:
	@if [ -x "$(GO)" ]; then \
		"$(GO)" version; \
	else \
		mkdir -p "$(TOOLS)"; \
		tmp=$$(mktemp -d); \
		os=$$(uname -s | tr '[:upper:]' '[:lower:]'); \
		arch=$$(uname -m); \
		case "$$arch" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; esac; \
		file="go$(GO_VERSION).$$os-$$arch.tar.gz"; \
		echo "Downloading $$file"; \
		curl -fsSL "https://go.dev/dl/$$file" -o "$$tmp/$$file"; \
		rm -rf "$(GOROOT)"; \
		tar -C "$(TOOLS)" -xzf "$$tmp/$$file"; \
		rm -rf "$$tmp"; \
		"$(GO)" version; \
	fi
