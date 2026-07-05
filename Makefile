APP := fragata
VERSION ?= dev
GOFLAGS_STATIC := -trimpath -tags netgo,osusergo
LDFLAGS := -s -w -buildid= -X main.version=$(VERSION)

.PHONY: fmt test vet build build-linux-amd64 build-linux-arm64 verify-static clean

fmt:
	gofmt -w cmd internal

test:
	go test ./...

vet:
	go vet ./...

build:
	mkdir -p dist
	CGO_ENABLED=0 go build $(GOFLAGS_STATIC) -ldflags="$(LDFLAGS)" -o dist/$(APP) ./cmd/fragata

build-linux-amd64:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GOFLAGS_STATIC) -ldflags="$(LDFLAGS)" -o dist/$(APP)-linux-amd64 ./cmd/fragata

build-linux-arm64:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build $(GOFLAGS_STATIC) -ldflags="$(LDFLAGS)" -o dist/$(APP)-linux-arm64 ./cmd/fragata

verify-static: build-linux-amd64
	file dist/$(APP)-linux-amd64
	@if command -v ldd >/dev/null 2>&1; then ldd dist/$(APP)-linux-amd64 2>&1 | grep -Eq "not a dynamic executable|statically linked"; fi

clean:
	rm -rf dist build coverage
