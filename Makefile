.PHONY: build run test clean docker

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags "-s -w -X github.com/fusionn-air/internal/version.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o fusionn-air ./cmd/fusionn

run:
	go run ./cmd/fusionn

test:
	go test -v ./...

clean:
	rm -f fusionn-air

docker:
	docker build --build-arg VERSION=$(VERSION) -t fusionn-air:$(VERSION) .

docker-run:
	docker compose up -d

docker-logs:
	docker compose logs -f

docker-stop:
	docker compose down

