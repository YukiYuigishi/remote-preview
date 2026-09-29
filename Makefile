GO ?= go
BINDIR ?= bin
SERVER_OS ?= all

ifeq ($(SERVER_OS),all)
SERVER_TAGS :=
else ifneq ($(filter $(SERVER_OS),linux darwin windows),)
SERVER_TAGS := -tags server_$(SERVER_OS)
else
$(error SERVER_OS must be all, linux, darwin, or windows)
endif

ifeq ($(OS),Windows_NT)
EXE := .exe
endif

APP := $(BINDIR)/ykview$(EXE)
HELPER := $(BINDIR)/ykview-helper$(EXE)

.PHONY: all build build-helper install generate test test-race vet check clean

all: build

build:
	mkdir -p "$(BINDIR)"
	$(GO) build $(SERVER_TAGS) -trimpath -o "$(APP)" ./cmd/ykview

build-helper:
	mkdir -p "$(BINDIR)"
	$(GO) build -trimpath -o "$(HELPER)" ./cmd/ykview-helper

install:
	$(GO) install $(SERVER_TAGS) -trimpath ./cmd/ykview

generate:
	GO="$(GO)" $(GO) generate ./internal/preview

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

check:
	$(GO) test ./...
	$(GO) test -race ./...
	$(GO) vet ./...
	mkdir -p "$(BINDIR)"
	$(GO) build $(SERVER_TAGS) -trimpath -o "$(APP)" ./cmd/ykview

clean:
	rm -f "$(APP)" "$(HELPER)"
