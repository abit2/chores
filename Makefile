BIN := chores
DIST := dist
VERSION ?= $(shell git describe --tags --always --dirty)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all build dist test clean install

all: build

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

dist:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BIN)-darwin-arm64 .
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o $(DIST)/$(BIN)-darwin-amd64 .
	cd $(DIST) && shasum -a 256 $(BIN)-darwin-arm64 $(BIN)-darwin-amd64 > SHA256SUMS

test:
	go test ./...

clean:
	rm -f $(BIN)
	rm -rf $(DIST)

install: build
	install -d $(shell go env GOPATH)/bin
	install -m 755 $(BIN) $(shell go env GOPATH)/bin/$(BIN)
