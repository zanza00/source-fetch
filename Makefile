.PHONY: build test smoke vet fmt release

build:
	go build -o source-fetch ./cmd/source-fetch

test:
	go test ./...

smoke:
	test/run.sh

vet:
	go vet ./...

fmt:
	gofmt -w .

release:
	goreleaser release --clean
