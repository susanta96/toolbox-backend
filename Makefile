.PHONY: build run dev test clean docker-build docker-run lint tidy

# Variables
APP_NAME = toolbox-backend
BINARY   = ./bin/$(APP_NAME)
CMD_DIR  = ./cmd/server

## build: Build the binary
build:
	@echo "Building $(APP_NAME)..."
	go build -ldflags="-s -w" -o $(BINARY) $(CMD_DIR)

## run: Run the application locally
run:
	go run $(CMD_DIR)

## dev: Run with hot-reload (requires: go install github.com/air-verse/air@latest)
dev:
	air

## test: Run all tests
test:
	go test -v -race -coverprofile=coverage.out ./...

## coverage: Show test coverage
coverage: test
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

## lint: Run golangci-lint
lint:
	golangci-lint run ./...

## tidy: Tidy and verify dependencies
tidy:
	go mod tidy
	go mod verify

## clean: Remove build artifacts
clean:
	rm -rf bin/ coverage.out coverage.html uploads/ generated/

## docker-build: Build Docker image
docker-build:
	docker build -t $(APP_NAME):latest .

## docker-run: Run with Docker
docker-run:
	docker run --rm -p 8080:8080 --env-file .env $(APP_NAME):latest

## help: Show this help
help:
	@echo "Available targets:"
	@grep -E '^## ' Makefile | sed 's/## /  /'
