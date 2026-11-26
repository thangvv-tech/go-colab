GO ?= go
BIN_DIR ?= bin
BINARY := $(BIN_DIR)/colab

.PHONY: fmt test vet build verify integration release-snapshot

fmt:
	gofmt -w colab/*.go cmd/colab/*.go

test:
	$(GO) test ./... -v

vet:
	$(GO) vet ./...

build:
	mkdir -p $(BIN_DIR)
	$(GO) build -o $(BINARY) ./cmd/colab

verify: fmt test vet build
	$(GO) mod verify
	git diff --check

integration:
	COLAB_INTEGRATION=1 $(GO) test -tags integration ./colab -run TestRuntimeRoundTrip -v

release-snapshot:
	goreleaser release --snapshot --clean
