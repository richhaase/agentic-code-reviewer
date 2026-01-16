# Agentic Code Reviewer development tasks

.PHONY: help build test test-coverage fmt lint vet tidy clean find-deadcode staticcheck check

# Show available targets
help:
	@echo "Available targets:"
	@echo "  build        - Build the acr binary with version information"
	@echo "  test         - Run all unit tests"
	@echo "  test-coverage - Run tests with coverage"
	@echo "  fmt          - Format Go source code"
	@echo "  lint         - Run golangci-lint v2"
	@echo "  vet          - Run go vet"
	@echo "  tidy         - Tidy go modules"
	@echo "  clean        - Clean build artifacts and test cache"
	@echo "  find-deadcode - Run dead code analysis"
	@echo "  staticcheck  - Run staticcheck"
	@echo "  check        - Run all quality checks (fmt, lint, vet, staticcheck, tests)"

# Build the acr binary with version information
build:
	@echo "Building acr with version information..."
	@mkdir -p bin
	@VERSION=$$(git describe --tags --always --dirty 2>/dev/null || echo "dev"); \
	COMMIT=$$(git rev-parse --short HEAD 2>/dev/null || echo "none"); \
	DATE=$$(date -u +"%Y-%m-%dT%H:%M:%SZ"); \
	if ! go build -ldflags "-X main.version=$$VERSION -X main.commit=$$COMMIT -X main.date=$$DATE" -o bin/acr ./cmd/acr; then \
		echo "Build failed"; \
		exit 1; \
	fi; \
	echo "Built versioned acr binary to bin/ (version: $$VERSION)"

# Run all unit tests
test:
	@echo "Running unit tests..."
	@go test ./...
	@echo "Unit tests passed!"

# Run tests with coverage
test-coverage:
	@echo "Running unit tests with coverage..."
	@go clean -testcache
	@go test -covermode=atomic -coverprofile=coverage.out ./...
	@go tool cover -func=coverage.out > coverage.txt
	@awk 'END{printf "Total coverage: %s\n", $$3}' coverage.txt
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Unit tests passed! Coverage report: coverage.html (see also coverage.txt)"

# Format Go source code
fmt:
	@echo "Formatting Go source code..."
	@go fmt ./...
	@echo "Formatting complete!"

# Run golangci-lint v2
lint:
	@echo "Running golangci-lint v2..."
	@go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.8.0 run --timeout=10m ./...
	@echo "Linting passed!"

# Run go vet
vet:
	@echo "Running go vet..."
	@go vet ./...
	@echo "Vet passed!"

# Tidy go modules
tidy:
	@echo "Tidying go modules..."
	@go mod tidy
	@echo "Modules tidied!"

# Clean build artifacts and test cache
clean:
	@echo "Cleaning build artifacts and caches..."
	@rm -rf bin
	@rm -f coverage.out coverage.html coverage.txt
	@go clean
	@go clean -testcache
	@echo "Build artifacts and test cache cleaned"

# Run dead code analysis
find-deadcode:
	@echo "Search for dead code..."
	@go run golang.org/x/tools/cmd/deadcode@latest ./...
	@echo "Done"

# Run staticcheck
staticcheck:
	@echo "Running staticcheck..."
	@go run honnef.co/go/tools/cmd/staticcheck@latest ./...
	@echo "Staticcheck passed!"

# Run all quality checks (format, lint, vet, staticcheck, tests)
check: fmt lint vet staticcheck test
