.PHONY: build run test test-cover clean lint tidy docker docker-run docker-logs docker-stop tools generate

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X github.com/fusionn-air/internal/version.Version=$(VERSION)"

# Build binary
build:
	go build $(LDFLAGS) -o fusionn-air ./cmd/fusionn-air

# Run locally
run:
	go run ./cmd/fusionn-air

# Run tests
test:
	go test -v -race ./...

# Run tests with coverage
test-cover:
	go test -v -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

# Clean build artifacts
clean:
	rm -f fusionn-air
	rm -f coverage.out coverage.html

# Run linter
lint:
	golangci-lint run ./...

# Tidy dependencies
tidy:
	go mod tidy

# Docker build
docker:
	docker build --build-arg VERSION=$(VERSION) -t fusionn-air:$(VERSION) .

# Docker compose commands
docker-run:
	docker compose up -d

docker-logs:
	docker compose logs -f

docker-stop:
	docker compose down

# Install dev tools
tools:
	go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.0

# Run code generation
generate:
	go generate ./...
