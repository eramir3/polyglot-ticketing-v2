.PHONY: install generate-proto build build-api-gateway build-identity test test-api-gateway serve-api-gateway serve-identity

install: ## Install workspace dependencies.
	pnpm install --frozen-lockfile

generate-proto: ## Generate TypeScript and Go protobuf bindings.
	pnpm proto:generate

build: build-api-gateway build-identity ## Build every service.

build-api-gateway: ## Build the API gateway.
	pnpm nx build api-gateway

build-identity: ## Build the identity service.
	pnpm nx build identity

test: test-api-gateway ## Run all executable tests.

test-api-gateway: ## Run API gateway integration tests (requires Docker for Testcontainers).
	pnpm nx run api-gateway:integration

serve-api-gateway: ## Run the API gateway locally.
	pnpm nx serve api-gateway

serve-identity: ## Run the identity gRPC service locally.
	pnpm nx serve identity

