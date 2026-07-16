VERSION ?= $(shell git describe --tags --dirty --always --match 'v*' 2>/dev/null | sed 's/^v//' || echo "dev")
COMMIT  ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "none")
DATE    ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || echo "unknown")
LDFLAGS := -ldflags="-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.date=$(DATE)"
BINARY  ?= qodex

.PHONY: build test vet clean tidy release snapshot install build-all

build:
	go build $(LDFLAGS) -o $(BINARY) ./cmd/qodex

test:
	go test -race -count=1 ./...

vet:
	go vet ./...

tidy:
	go mod tidy

clean:
	rm -f $(BINARY)
	rm -rf dist/

install: build
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 755 $(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)

snapshot:
	goreleaser release --snapshot --clean

release:
	goreleaser release --clean

build-all:
	GOOS=linux   GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o build/qodex-linux-amd64   ./cmd/qodex
	GOOS=linux   GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o build/qodex-linux-arm64   ./cmd/qodex
	GOOS=darwin  GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o build/qodex-darwin-amd64  ./cmd/qodex
	GOOS=darwin  GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o build/qodex-darwin-arm64  ./cmd/qodex
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build $(LDFLAGS) -o build/qodex-windows-amd64.exe ./cmd/qodex
	GOOS=windows GOARCH=arm64 CGO_ENABLED=0 go build $(LDFLAGS) -o build/qodex-windows-arm64.exe ./cmd/qodex
