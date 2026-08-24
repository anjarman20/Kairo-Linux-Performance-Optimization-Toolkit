VERSION ?= 0.1.0
BIN     := bin/kairo

.PHONY: all build test vet fmt clean install

all: fmt vet test build

build:
	go build -o $(BIN) ./cmd/kairo

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

clean:
	rm -rf bin dist

install:
	go install ./cmd/kairo