VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS  := -trimpath -ldflags "-s -w -X main.version=$(VERSION)"
BINARY   := lnk
CMD      := ./cmd/lnk/

.PHONY: build install test clean fmt vet lint ci

## build: Compile lnk into the current directory.
build:
	go build $(LDFLAGS) -o $(BINARY) $(CMD)

## install: Install lnk into $GOPATH/bin (or ~/go/bin).
install:
	go install $(LDFLAGS) $(CMD)

## test: Run the full test suite.
test:
	go test -race ./...

## vet: Run go vet across all packages.
vet:
	go vet ./...

## fmt: Format all Go source files.
fmt:
	gofmt -w -s .

## lint: Run golangci-lint if available, otherwise go vet.
lint:
	@which golangci-lint > /dev/null 2>&1 && golangci-lint run ./... || go vet ./...

## ci: Run vet, fmt check, and tests (mirrors CI workflow).
ci: vet
	@test -z "$$(gofmt -l .)" || (echo "gofmt: unformatted files:" && gofmt -l . && exit 1)
	go test -race ./...

## clean: Remove the compiled binary.
clean:
	rm -f $(BINARY)

## help: Print this help message.
help:
	@echo "Usage: make <target>"
	@echo ""
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
