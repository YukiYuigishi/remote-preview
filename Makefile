GO ?= go
BINDIR ?= bin

APP := $(BINDIR)/ykview
HELPER := $(BINDIR)/ykview-helper

.PHONY: all build build-helper install generate test test-race vet check clean

all: build

build: generate
	mkdir -p "$(BINDIR)"
	$(GO) build -trimpath -o "$(APP)" ./cmd/ykview

build-helper:
	mkdir -p "$(BINDIR)"
	$(GO) build -trimpath -o "$(HELPER)" ./cmd/ykview-helper

install:
	$(GO) install -trimpath ./cmd/ykview

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
	$(GO) build -trimpath -o "$(APP)" ./cmd/ykview

clean:
	rm -f "$(APP)" "$(HELPER)"
