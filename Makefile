.PHONY: build test docker-build clean

BUILD_GOCACHE ?= $(CURDIR)/.cache/go-build

build:
	GOCACHE=$(BUILD_GOCACHE) go build -buildvcs=false -o bin/experiment-executor ./cmd/experiment-executor

test:
	GOCACHE=$(BUILD_GOCACHE) go test ./...

docker-build:
	docker build -t experiment-executor:local .

clean:
	rm -rf bin .cache
