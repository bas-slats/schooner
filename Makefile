.PHONY: all build test fmt vet lint clean install-hooks run

# Default target
all: fmt vet test build

# Build the binary
build:
	go build -o schooner ./cmd/schooner

# Run tests
test:
	go test ./...

# Run tests with coverage
test-coverage:
	go test -cover ./...

# Format code
fmt:
	go fmt ./...

# Vet for issues
vet:
	go vet ./...

# Run all linting checks
lint: fmt vet
	@echo "Linting complete"

# Clean build artifacts
clean:
	rm -f schooner

# Install git hooks
install-hooks:
	cp scripts/pre-commit .git/hooks/pre-commit
	chmod +x .git/hooks/pre-commit
	@echo "Git hooks installed"

# Run the application
run: build
	./schooner

# Pre-commit checks (same as hooks)
pre-commit: fmt vet test build
	@echo "Pre-commit checks passed"
