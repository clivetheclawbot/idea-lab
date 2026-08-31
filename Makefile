BINARY := idea-lab

.PHONY: check test build fmt

build:
	go build -o $(BINARY) .

test:
	go test -race ./...

fmt:
	gofmt -l .

# One entry point rather than four bare go commands (house convention).
check: test
	go vet ./...
	go build -o $(BINARY) .
	@out=$$(gofmt -l .); [ -z "$$out" ] || { echo "unformatted:"; echo "$$out"; exit 1; }
	git diff --check