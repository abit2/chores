BIN := chores

.PHONY: all build test clean install

all: build

build:
	go build -o $(BIN) .

test:
	go test ./...

clean:
	rm -f $(BIN)

install: build
	install -d $(shell go env GOPATH)/bin
	install -m 755 $(BIN) $(shell go env GOPATH)/bin/$(BIN)
