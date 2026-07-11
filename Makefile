BINARY := restic-reporter
# Target host (gaming-server) is a linux/amd64 vSphere VM.
GOOS   ?= linux
GOARCH ?= amd64

.PHONY: build
build:
	CGO_ENABLED=0 GOOS=$(GOOS) GOARCH=$(GOARCH) go build -trimpath -ldflags="-s -w" -o dist/$(BINARY) .

.PHONY: test
test:
	go test ./...

.PHONY: vet
vet:
	go vet ./...

.PHONY: clean
clean:
	rm -rf dist
