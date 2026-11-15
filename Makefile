.PHONY: tidy migrate-up api worker seed test docker-up docker-down web-install web-dev demo

tidy:
	go mod tidy

migrate-up:
	go run ./cmd/migrate

api:
	go run ./cmd/api

worker:
	go run ./cmd/worker

seed:
	go run ./cmd/seed

test:
	go test ./...

docker-up:
	docker compose up --build -d

docker-down:
	docker compose down -v

web-install:
	cd web && npm install

web-dev:
	cd web && npm run dev

# One-command local stack + seed credentials printed by the API container.
demo: docker-up
	@echo ""
	@echo "Attest is starting."
	@echo "  API:  http://localhost:8080/healthz"
	@echo "  UI:   http://localhost:3000"
	@echo "  Demo login: demo@attest.dev / AttestDemo!123"
	@echo ""
