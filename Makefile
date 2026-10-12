VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || cat VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test dist clean
build:
	CGO_ENABLED=0 go build -ldflags="$(LDFLAGS)" -o sahran .

install: build
	./sahran install

test:
	go vet ./... && go test ./...

dist:
	mkdir -p dist
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o dist/sahran-linux-amd64 .
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -ldflags="$(LDFLAGS)" -o dist/sahran-linux-arm64 .

clean:
	rm -rf sahran dist
