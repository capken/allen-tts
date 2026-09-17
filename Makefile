VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo 0.1.0-dev)
LDFLAGS  = -s -w -X github.com/capken/allen-tts/internal/version.Version=$(VERSION)
BIN      = allen-tts

.PHONY: build test vet fmt cross clean

build:
	go build -ldflags '$(LDFLAGS)' -o bin/$(BIN) ./cmd/allen-tts

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

# 交叉编译 darwin/linux × amd64/arm64
cross:
	GOOS=darwin  GOARCH=amd64 go build -ldflags '$(LDFLAGS)' -o bin/$(BIN)-darwin-amd64  ./cmd/allen-tts
	GOOS=darwin  GOARCH=arm64 go build -ldflags '$(LDFLAGS)' -o bin/$(BIN)-darwin-arm64  ./cmd/allen-tts
	GOOS=linux   GOARCH=amd64 go build -ldflags '$(LDFLAGS)' -o bin/$(BIN)-linux-amd64   ./cmd/allen-tts
	GOOS=linux   GOARCH=arm64 go build -ldflags '$(LDFLAGS)' -o bin/$(BIN)-linux-arm64   ./cmd/allen-tts

clean:
	rm -rf bin
