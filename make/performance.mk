.PHONY: validate-k6-tickets-list-profile validate-k6-tickets-create-profile validate-k6-tickets-create-update-profile prepare-k6-tickets-list k6-tickets-list seed-performance-tickets prepare-k6-tickets-create k6-tickets-create prepare-k6-tickets-create-update k6-tickets-create-update

K6_PROFILE ?= smoke
K6_EXPECT_TICKET_COUNT ?= 100

validate-k6-tickets-list-profile:
	@node -e 'const configs = require("./tests/k6/tickets/list-configs.json"); const profile = process.argv[1]; if (!Object.prototype.hasOwnProperty.call(configs, profile)) { console.error("K6_PROFILE must be one of: " + Object.keys(configs).join(", ")); process.exit(1); }' "$(K6_PROFILE)"

validate-k6-tickets-create-profile:
	@node -e 'const configs = require("./tests/k6/tickets/create-configs.json"); const profile = process.argv[1]; if (!Object.prototype.hasOwnProperty.call(configs, profile)) { console.error("K6_PROFILE must be one of: " + Object.keys(configs).join(", ")); process.exit(1); }' "$(K6_PROFILE)"

validate-k6-tickets-create-update-profile:
	@node -e 'const configs = require("./tests/k6/tickets/create-update-configs.json"); const profile = process.argv[1]; if (!Object.prototype.hasOwnProperty.call(configs, profile)) { console.error("K6_PROFILE must be one of: " + Object.keys(configs).join(", ")); process.exit(1); }' "$(K6_PROFILE)"

prepare-k6-tickets-list: ## Reset local data, start the stack, and seed the 100-ticket k6 dataset.
	$(MAKE) docker-reset
	$(MAKE) seed-performance-tickets

k6-tickets-list: validate-k6-tickets-list-profile ## Run the selected k6 ticket-list profile (K6_PROFILE=smoke|load|stress).
	docker compose --profile performance run --rm k6 run -e K6_EXPECT_TICKET_COUNT=$(K6_EXPECT_TICKET_COUNT) -e K6_PROFILE=$(K6_PROFILE) /scripts/tickets/list.js

prepare-k6-tickets-create: ## Reset local data and start the stack for ticket-create k6 tests.
	$(MAKE) docker-reset

k6-tickets-create: validate-k6-tickets-create-profile ## Run the selected k6 ticket-create profile (K6_PROFILE=smoke|load|stress).
	docker compose --profile performance run --rm k6 run -e K6_PROFILE=$(K6_PROFILE) /scripts/tickets/create.js

prepare-k6-tickets-create-update: ## Reset local data and start the stack for ticket create-and-update k6 tests.
	$(MAKE) docker-reset

k6-tickets-create-update: validate-k6-tickets-create-update-profile ## Run the selected k6 ticket create-and-update profile (K6_PROFILE=smoke|load|stress).
	docker compose --profile performance run --rm k6 run -e K6_PROFILE=$(K6_PROFILE) /scripts/tickets/create-update.js

seed-performance-tickets: ## Seed exactly 100 tickets into an empty local tickets-db.
	docker compose --profile performance run --rm tickets-performance-seed
