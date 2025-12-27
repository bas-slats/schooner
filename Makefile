# Homelab CD Makefile

.PHONY: all build run test clean docker docker-build docker-run help

# Variables
BINARY_NAME=homelab-cd
VERSION?=$(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME=$(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS=-ldflags "-w -s -X main.version=$(VERSION)"

# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GORUN=$(GOCMD) run
GOTEST=$(GOCMD) test
GOGET=$(GOCMD) get
GOMOD=$(GOCMD) mod

# Default target
all: build

## build: Build the binary
build:
	CGO_ENABLED=1 $(GOBUILD) $(LDFLAGS) -o bin/$(BINARY_NAME) ./cmd/homelab-cd

## run: Run the application
run:
	$(GORUN) ./cmd/homelab-cd

## test: Run tests
test:
	$(GOTEST) -v ./...

## test-coverage: Run tests with coverage
test-coverage:
	$(GOTEST) -v -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html

## clean: Clean build artifacts
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

## deps: Download dependencies
deps:
	$(GOMOD) download
	$(GOMOD) tidy

## docker-build: Build Docker image
docker-build:
	docker build -t $(BINARY_NAME):$(VERSION) --build-arg VERSION=$(VERSION) .

## docker-run: Run with Docker Compose
docker-run:
	docker compose up -d

## docker-stop: Stop Docker Compose
docker-stop:
	docker compose down

## docker-logs: View Docker logs
docker-logs:
	docker compose logs -f

## lint: Run linter
lint:
	golangci-lint run ./...

## fmt: Format code
fmt:
	$(GOCMD) fmt ./...

## vet: Run go vet
vet:
	$(GOCMD) vet ./...

## dev: Run with hot reload (requires air)
dev:
	air

## init-db: Initialize the database
init-db:
	mkdir -p data
	sqlite3 data/homelab-cd.db < migrations/001_initial_schema.sql

## help: Show this help
help:
	@echo "Homelab CD - Available targets:"
	@echo ""
	@sed -n 's/^##//p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/ /'
