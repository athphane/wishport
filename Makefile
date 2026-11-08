.PHONY: build test vet lint check release-check release-snapshot
build:
	go build -trimpath -o wishport ./cmd/wishport
test:
	go test -race ./...
vet:
	go vet ./...
lint:
	go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
check: test vet lint
release-check:
	goreleaser check
release-snapshot:
	goreleaser release --snapshot --clean
