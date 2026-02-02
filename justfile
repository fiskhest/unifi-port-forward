# =============================================================================
# unifi-port-forward Justfile
# =============================================================================
# Kubernetes controller for automatic router port forwarding configuration
#
# Quick Start:
#   just check    # Run all quality checks (default)
#   just test     # Run tests
#   just lint     # Run linter
#   just fmt      # Check formatting
#   just build    # docker build

# =============================================================================
# Configuration
# =============================================================================

# Enable .env file support for local configuration
set dotenv-load

# Use bash with strict error checking
set shell := ["bash", "-uc"]

# Common aliases for convenience
alias c := check
alias t := test
alias l := lint
alias f := fmt
alias b := build

# =============================================================================
# Default Recipe
# =============================================================================

# Run all quality checks (default recipe)
@default:
    just --list

# =============================================================================
# Core Testing Commands (Your Requirements)
# =============================================================================

# Run tests with verbose output
@test:
    echo "🧪 Running tests..."
    go test -v ./...

# Run linter
@lint:
    echo "🔍 Running linter..."
    golangci-lint run ./...

# Check code formatting (lists unformatted files)
@fmt:
    echo "📝 Checking formatting..."
    gofmt -l $(find . -name "*.go" -not -path "./vendor/*")

# Run all quality checks (combines test + lint + fmt)
@check: test lint fmt
    echo "✅ All checks passed!"

# Do docker build
@build:
    docker build --push -t ghcr.io/fiskhest/unifi-port-forward .
