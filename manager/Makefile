.PHONY: api-bundle
api-bundle:
	cd api && ./bundle-spec.sh

.PHONY: api-generate
api-generate: api-bundle
	~/go/bin/oapi-codegen -generate chi-server,models,spec -package api api/api-spec.bundled.yaml > api/api.gen.go

.PHONY: postgres-generate
postgres-generate:
	cd store/postgres && sqlc generate

.PHONY: postgres-migrate-up
postgres-migrate-up:
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "ERROR: DATABASE_URL environment variable not set"; \
		echo "Example: export DATABASE_URL='postgres://user:password@localhost:5432/dbname?sslmode=disable'"; \
		exit 1; \
	fi
	migrate -path store/postgres/migrations -database "$(DATABASE_URL)" up

.PHONY: postgres-migrate-down
postgres-migrate-down:
	@if [ -z "$(DATABASE_URL)" ]; then \
		echo "ERROR: DATABASE_URL environment variable not set"; \
		echo "Example: export DATABASE_URL='postgres://user:password@localhost:5432/dbname?sslmode=disable'"; \
		exit 1; \
	fi
	migrate -path store/postgres/migrations -database "$(DATABASE_URL)" down

.PHONY: postgres-migrate-create
postgres-migrate-create:
	@if [ -z "$(NAME)" ]; then \
		echo "ERROR: NAME variable not set"; \
		echo "Usage: make postgres-migrate-create NAME=description_here"; \
		exit 1; \
	fi
	migrate create -ext sql -dir store/postgres/migrations -seq $(NAME)

.PHONY: postgres-test
postgres-test:
	go test -v -race -count=1 ./store/postgres/...

.PHONY: postgres-test-coverage
postgres-test-coverage:
	go test -v -race -coverprofile=coverage.out ./store/postgres/...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"
