.PHONY: build test clean

build:
	go build ./...

test:
	go test ./... -v

clean:
	rm -f bin/*
