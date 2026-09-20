GO ?= go
BINDIR ?= bin

APP := $(BINDIR)/remote-preview
HELPER := $(BINDIR)/remote-preview-helper

.PHONY: all build build-helper generate test test-race vet check clean

all: build

build: generate
	mkdir -p "$(BINDIR)"
	$(GO) build -trimpath -o "$(APP)" ./cmd/remote-preview

build-helper:
	mkdir -p "$(BINDIR)"
	$(GO) build -trimpath -o "$(HELPER)" ./cmd/remote-preview-helper

generate:
	GO="$(GO)" $(GO) generate ./internal/preview

test:
	$(GO) test ./...

test-race:
	$(GO) test -race ./...

vet:
	$(GO) vet ./...

check: generate
	$(GO) test ./...
	$(GO) test -race ./...
	$(GO) vet ./...
	mkdir -p "$(BINDIR)"
	$(GO) build -trimpath -o "$(APP)" ./cmd/remote-preview

clean:
	rm -f "$(APP)" "$(HELPER)"
