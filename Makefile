BINARY := restic-reporter
PKG    := github.com/andrew-avinante/restic-reporter/cmd
# Target host (gaming-server) is a linux/amd64 vSphere VM.
GOOS   ?= linux
GOARCH ?= amd64
# Version stamped into `restic-reporter version`; defaults to the git describe.
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)

.PHONY: build
build:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -trimpath \
		-ldflags="-s -w -X $(PKG).version=$(VERSION)" -o dist/$(BINARY) .

.PHONY: test
test:
	go test ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: clean
clean:
	rm -rf dist
