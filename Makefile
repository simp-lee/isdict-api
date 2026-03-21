.PHONY: help build run test test-coverage lint clean deps tidy db-setup db-verify db-reset db-fixtures dev

# Variables
APP_NAME := isdict-api
MAIN_PATH := ./cmd/api
BIN_DIR := ./bin
ISDICT_API_ENV_FILE ?= configs/api.env

define resolve_db_env
ISDICT_API_ENV_FILE_VALUE="$(ISDICT_API_ENV_FILE)"; \
env_file_value() { \
	key="$$1"; \
	if [ -z "$$ISDICT_API_ENV_FILE_VALUE" ] || [ ! -f "$$ISDICT_API_ENV_FILE_VALUE" ]; then \
		return 0; \
	fi; \
	sed -n "/^[[:space:]]*$$key[[:space:]]*=/ { s/^[^=]*=//; s/\r$$//; p; q; }" "$$ISDICT_API_ENV_FILE_VALUE"; \
}; \
DB_HOST_VALUE="$${DB_HOST:-$${PGHOST:-$$(env_file_value DB_HOST)}}"; \
DB_PORT_VALUE="$${DB_PORT:-$${PGPORT:-$$(env_file_value DB_PORT)}}"; \
DB_USER_VALUE="$${DB_USER:-$${PGUSER:-$$(env_file_value DB_USER)}}"; \
DB_PASSWORD_VALUE="$${DB_PASSWORD:-$${PGPASSWORD:-$$(env_file_value DB_PASSWORD)}}"; \
DB_NAME_VALUE="$${DB_NAME:-$${PGDATABASE:-$$(env_file_value DB_NAME)}}"; \
DB_SSLMODE_VALUE="$${DB_SSLMODE:-$${PGSSLMODE:-$$(env_file_value DB_SSLMODE)}}"; \
if [ -z "$$DB_SSLMODE_VALUE" ]; then \
	DB_SSLMODE_VALUE="prefer"; \
fi
endef

define resolve_test_postgres_env
$(resolve_db_env); \
TEST_POSTGRES_DSN_VALUE="$${TEST_POSTGRES_DSN:-}"; \
TEST_POSTGRES_ADMIN_DSN_VALUE="$${TEST_POSTGRES_ADMIN_DSN:-}"; \
if [ -z "$$TEST_POSTGRES_DSN_VALUE" ]; then \
	if [ -z "$$DB_HOST_VALUE" ] || [ -z "$$DB_PORT_VALUE" ] || [ -z "$$DB_USER_VALUE" ] || [ -z "$$DB_NAME_VALUE" ]; then \
		echo "error: make test requires TEST_POSTGRES_DSN, or DB_HOST/DB_PORT/DB_USER/DB_NAME (or PGHOST/PGPORT/PGUSER/PGDATABASE), or point ISDICT_API_ENV_FILE to an env file before running tests"; \
		exit 1; \
	fi; \
	TEST_POSTGRES_DSN_VALUE="host=$$DB_HOST_VALUE port=$$DB_PORT_VALUE user=$$DB_USER_VALUE"; \
	if [ -n "$$DB_PASSWORD_VALUE" ]; then \
		TEST_POSTGRES_DSN_VALUE="$$TEST_POSTGRES_DSN_VALUE password=$$DB_PASSWORD_VALUE"; \
	fi; \
	TEST_POSTGRES_DSN_VALUE="$$TEST_POSTGRES_DSN_VALUE dbname=$$DB_NAME_VALUE sslmode=$$DB_SSLMODE_VALUE"; \
fi; \
if [ -z "$$TEST_POSTGRES_ADMIN_DSN_VALUE" ]; then \
	TEST_POSTGRES_ADMIN_DSN_VALUE="$$TEST_POSTGRES_DSN_VALUE"; \
fi
endef

define verify_test_postgres_prerequisites
if ! TEST_POSTGRES_ADMIN_ROLE_CAPS_VALUE="$$(psql -v ON_ERROR_STOP=1 "$$TEST_POSTGRES_ADMIN_DSN_VALUE" -tA -c "SELECT CASE WHEN rolcreatedb THEN 'ok' ELSE 'createdb=' || rolcreatedb::text END FROM pg_roles WHERE rolname = current_user")"; then \
	echo "error: unable to verify TEST_POSTGRES_ADMIN_DSN PostgreSQL privileges for make test"; \
	exit 1; \
fi; \
if [ "$$TEST_POSTGRES_ADMIN_ROLE_CAPS_VALUE" != "ok" ]; then \
	echo "error: make test requires TEST_POSTGRES_ADMIN_DSN current_user to have CREATEDB; got $$TEST_POSTGRES_ADMIN_ROLE_CAPS_VALUE"; \
	exit 1; \
fi
endef

## help: Show this help message
help:
	@echo 'Usage:'
	@sed -n 's/^##//p' ${MAKEFILE_LIST} | column -t -s ':' | sed -e 's/^/ /'

## build: Build the application binary
build:
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BIN_DIR)
	@go build -o $(BIN_DIR)/$(APP_NAME) $(MAIN_PATH)
	@echo "Build complete: $(BIN_DIR)/$(APP_NAME)"

## run: Run the application
run:
	@echo "Running $(APP_NAME)..."
	@ISDICT_API_ENV_FILE="$(ISDICT_API_ENV_FILE)" go run $(MAIN_PATH)

## test: Run all tests
test:
	@echo "Running tests..."
	@node --test web/dictionary_app.test.mjs
	@$(resolve_test_postgres_env); \
	$(verify_test_postgres_prerequisites); \
	TEST_POSTGRES_DSN="$$TEST_POSTGRES_DSN_VALUE" TEST_POSTGRES_ADMIN_DSN="$$TEST_POSTGRES_ADMIN_DSN_VALUE" \
	go test -v -race ./...

## test-coverage: Run tests with coverage report
test-coverage:
	@echo "Running tests with coverage..."
	@node --test web/dictionary_app.test.mjs
	@$(resolve_test_postgres_env); \
	$(verify_test_postgres_prerequisites); \
	TEST_POSTGRES_DSN="$$TEST_POSTGRES_DSN_VALUE" TEST_POSTGRES_ADMIN_DSN="$$TEST_POSTGRES_ADMIN_DSN_VALUE" \
	go test -v -race -coverprofile=coverage.out -covermode=atomic ./...
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

## db-setup: Setup database using the authoritative Go migrations
db-setup:
	@echo "Setting up database..."
	@ISDICT_API_ENV_FILE="$(ISDICT_API_ENV_FILE)" go run ./cmd/migrate-db
	@echo "Database setup complete"

## db-verify: Verify migration-managed database objects using the authoritative Go migrations
db-verify:
	@echo "Verifying database migration state..."
	@ISDICT_API_ENV_FILE="$(ISDICT_API_ENV_FILE)" go run ./cmd/migrate-db --verify
	@echo "Database verification complete"

## db-reset: Drop and recreate database using DB_*/PG* or ISDICT_API_ENV_FILE
db-reset:
	@echo "Resetting database through guarded migration CLI..."
	@$(resolve_db_env); \
	if [ -z "$$DB_HOST_VALUE" ] || [ -z "$$DB_PORT_VALUE" ] || [ -z "$$DB_USER_VALUE" ] || [ -z "$$DB_NAME_VALUE" ]; then \
		echo "error: set DB_HOST/DB_PORT/DB_USER/DB_NAME (or PGHOST/PGPORT/PGUSER/PGDATABASE), or point ISDICT_API_ENV_FILE to an env file before running db-reset"; \
		exit 1; \
	fi; \
	EXPECTED_CONFIRM_VALUE="$$DB_USER_VALUE@$$DB_HOST_VALUE:$$DB_PORT_VALUE/$$DB_NAME_VALUE"; \
	CONFIRM_DROP_VALUE="$${CONFIRM_DROP:-}"; \
	if [ -z "$$CONFIRM_DROP_VALUE" ]; then \
		echo "error: set CONFIRM_DROP to $$EXPECTED_CONFIRM_VALUE"; \
		exit 1; \
	fi; \
	if [ "$$CONFIRM_DROP_VALUE" != "$$EXPECTED_CONFIRM_VALUE" ]; then \
		echo "error: CONFIRM_DROP must exactly match $$EXPECTED_CONFIRM_VALUE"; \
		exit 1; \
	fi; \
	DB_HOST="$$DB_HOST_VALUE" DB_PORT="$$DB_PORT_VALUE" DB_USER="$$DB_USER_VALUE" DB_PASSWORD="$$DB_PASSWORD_VALUE" DB_NAME="$$DB_NAME_VALUE" DB_SSLMODE="$$DB_SSLMODE_VALUE" \
	PGHOST="$$DB_HOST_VALUE" PGPORT="$$DB_PORT_VALUE" PGUSER="$$DB_USER_VALUE" PGPASSWORD="$$DB_PASSWORD_VALUE" PGDATABASE="$$DB_NAME_VALUE" PGSSLMODE="$$DB_SSLMODE_VALUE" \
	go run ./cmd/migrate-db --drop --force --confirm-drop "$$CONFIRM_DROP_VALUE"

## db-fixtures: Load sample fixtures using DB_*/PG* or ISDICT_API_ENV_FILE (requires CONFIRM_FIXTURES)
db-fixtures:
	@echo "Loading sample fixtures..."
	@$(resolve_db_env); \
	if [ -z "$$DB_HOST_VALUE" ] || [ -z "$$DB_PORT_VALUE" ] || [ -z "$$DB_USER_VALUE" ] || [ -z "$$DB_NAME_VALUE" ]; then \
		echo "error: set DB_HOST/DB_PORT/DB_USER/DB_NAME (or PGHOST/PGPORT/PGUSER/PGDATABASE), or point ISDICT_API_ENV_FILE to an env file before running db-fixtures"; \
		exit 1; \
	fi; \
	EXPECTED_CONFIRM_VALUE="$$DB_USER_VALUE@$$DB_HOST_VALUE:$$DB_PORT_VALUE/$$DB_NAME_VALUE"; \
	CONFIRM_FIXTURES_VALUE="$${CONFIRM_FIXTURES:-}"; \
	if [ -z "$$CONFIRM_FIXTURES_VALUE" ]; then \
		echo "error: set CONFIRM_FIXTURES to $$EXPECTED_CONFIRM_VALUE"; \
		exit 1; \
	fi; \
	if [ "$$CONFIRM_FIXTURES_VALUE" != "$$EXPECTED_CONFIRM_VALUE" ]; then \
		echo "error: CONFIRM_FIXTURES must exactly match $$EXPECTED_CONFIRM_VALUE"; \
		exit 1; \
	fi; \
	HAS_WORDS_VALUE="$$(DB_HOST="$$DB_HOST_VALUE" DB_PORT="$$DB_PORT_VALUE" DB_USER="$$DB_USER_VALUE" DB_PASSWORD="$$DB_PASSWORD_VALUE" DB_NAME="$$DB_NAME_VALUE" DB_SSLMODE="$$DB_SSLMODE_VALUE" \
	PGHOST="$$DB_HOST_VALUE" PGPORT="$$DB_PORT_VALUE" PGUSER="$$DB_USER_VALUE" PGPASSWORD="$$DB_PASSWORD_VALUE" PGDATABASE="$$DB_NAME_VALUE" PGSSLMODE="$$DB_SSLMODE_VALUE" \
	psql -v ON_ERROR_STOP=1 --host="$$DB_HOST_VALUE" --port="$$DB_PORT_VALUE" --username="$$DB_USER_VALUE" --dbname="$$DB_NAME_VALUE" -tA -c "SELECT CASE WHEN to_regclass('public.words') IS NULL THEN 'f' ELSE (EXISTS (SELECT 1 FROM words LIMIT 1))::text END")"; \
	if [ "$$HAS_WORDS_VALUE" = "t" ]; then \
		echo "error: db-fixtures only runs against an empty dictionary dataset; create a fresh disposable database or run db-reset first"; \
		exit 1; \
	fi; \
	echo "Resetting migration-managed schema before loading fixtures..."; \
	DB_HOST="$$DB_HOST_VALUE" DB_PORT="$$DB_PORT_VALUE" DB_USER="$$DB_USER_VALUE" DB_PASSWORD="$$DB_PASSWORD_VALUE" DB_NAME="$$DB_NAME_VALUE" DB_SSLMODE="$$DB_SSLMODE_VALUE" \
	PGHOST="$$DB_HOST_VALUE" PGPORT="$$DB_PORT_VALUE" PGUSER="$$DB_USER_VALUE" PGPASSWORD="$$DB_PASSWORD_VALUE" PGDATABASE="$$DB_NAME_VALUE" PGSSLMODE="$$DB_SSLMODE_VALUE" \
	ISDICT_API_ENV_FILE="$$ISDICT_API_ENV_FILE_VALUE" \
	go run ./cmd/migrate-db --drop --force --confirm-drop "$$CONFIRM_FIXTURES_VALUE"; \
	DB_HOST="$$DB_HOST_VALUE" DB_PORT="$$DB_PORT_VALUE" DB_USER="$$DB_USER_VALUE" DB_PASSWORD="$$DB_PASSWORD_VALUE" DB_NAME="$$DB_NAME_VALUE" DB_SSLMODE="$$DB_SSLMODE_VALUE" \
	PGHOST="$$DB_HOST_VALUE" PGPORT="$$DB_PORT_VALUE" PGUSER="$$DB_USER_VALUE" PGPASSWORD="$$DB_PASSWORD_VALUE" PGDATABASE="$$DB_NAME_VALUE" PGSSLMODE="$$DB_SSLMODE_VALUE" \
	psql -v ON_ERROR_STOP=1 --host="$$DB_HOST_VALUE" --port="$$DB_PORT_VALUE" --username="$$DB_USER_VALUE" --dbname="$$DB_NAME_VALUE" -f db/sample_data.sql
	@echo "Sample fixtures loaded"

## dev: Run in development mode with hot reload (requires air)
dev:
	@echo "Starting development server with hot reload..."
	@air

.DEFAULT_GOAL := help
