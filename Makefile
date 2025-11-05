.PHONY: help build run test clean docker-build docker-up docker-down db-setup

# Variables
APP_NAME := isdict-api
MAIN_PATH := ./cmd/api
BIN_DIR := ./bin
DOCKER_IMAGE := $(APP_NAME):latest

## help: Show this help message
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

## build: Build the application binary
build:
	@echo "Building $(APP_NAME)..."
	@go build -o $(BIN_DIR)/$(APP_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BIN_DIR)/$(APP_NAME)"

## run: Run the application
run:
	@echo "Running $(APP_NAME)..."
	@go run $(MAIN_PATH)

## test: Run all tests
test:
	@echo "Running tests..."
	@go test -v -race ./...

## test-coverage: Run tests with coverage report
test-coverage:
	@echo "Running tests with coverage..."
	@go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
	@go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

## lint: Run linters
lint:
	@echo "Running linters..."
	@go fmt ./...
	@go vet ./...

## clean: Clean build artifacts
clean:
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR)
	@rm -f coverage.out coverage.html
	@echo "Clean complete"

## deps: Download dependencies
deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod verify

## tidy: Tidy go.mod
tidy:
	@echo "Tidying go.mod..."
	@go mod tidy

## docker-build: Build Docker image
docker-build:
	@echo "Building Docker image..."
	@docker build -t $(DOCKER_IMAGE) .

## docker-up: Start services with Docker Compose
docker-up:
	@echo "Starting services..."
	@docker-compose up -d

## docker-down: Stop services
docker-down:
	@echo "Stopping services..."
	@docker-compose down

## docker-logs: View Docker logs
docker-logs:
	@docker-compose logs -f api

## db-setup: Setup database (create tables and indexes)
db-setup:
	@echo "Setting up database..."
	@psql -d isdict -f db/schema.sql
	@psql -d isdict -f db/indexes.sql
	@echo "Database setup complete"

## db-reset: Drop and recreate database
db-reset:
	@echo "Resetting database..."
	@dropdb --if-exists isdict
	@createdb isdict
	@make db-setup

## dev: Run in development mode with hot reload (requires air)
dev:
	@echo "Starting development server with hot reload..."
	@air

.DEFAULT_GOAL := help
