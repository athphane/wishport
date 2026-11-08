.PHONY: build test vet lint check
build:
	go build -trimpath -o wishport ./cmd/wishport
test:
	go test -race ./...
vet:
	go vet ./...
lint:
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
check: test vet lint
