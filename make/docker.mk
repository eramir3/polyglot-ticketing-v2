.PHONY: docker-build docker-up docker-up-tools docker-down docker-reset docker-logs docker-ps

docker-build: generate-proto ## Build all Docker Compose service images.
	docker compose build

docker-up: generate-proto ## Start the local Docker Compose stack and rebuild images.
	docker compose up -d --build

docker-down: ## Stop and remove the local Docker Compose stack.
	docker compose down

docker-reset: generate-proto ## Delete all application and optional-tool Compose data, then rebuild a fresh stack.
	docker compose down --volumes --remove-orphans
	docker compose up -d --build

docker-logs: ## Follow logs for the local Docker Compose stack.
	docker compose logs -f

docker-ps: ## Show local Docker Compose service status.
	docker compose ps
