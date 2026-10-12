.PHONY: build test smoke vet fmt release-check

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

release-check:
	goreleaser check
