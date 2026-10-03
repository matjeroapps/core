.PHONY: test go-test openapi openapi-check docker-config docker-build migrate-check migrate-up migrate-status

test: go-test openapi-check docker-config migrate-check

go-test:
	go test -timeout=20m ./...

openapi:
	go run ./cmd/openapi-gen

openapi-check: openapi
	git diff --exit-code -- docs/api

docker-config:
	docker compose config --quiet

docker-build:
	docker build -f docker/go-app.Dockerfile --build-arg APP_PATH=./apps/workers/general-worker -t commerce-general-worker:foundation .
	docker build -f docker/go-app.Dockerfile --build-arg APP_PATH=./apps/workers/scheduler -t commerce-scheduler:foundation .
	docker build -f docker/go-app.Dockerfile --build-arg APP_PATH=./apps/core-api -t commerce-core-api:foundation .
	docker build -f docker/go-app.Dockerfile --build-arg APP_PATH=./cmd/migrate -t commerce-core-migrate:foundation .

migrate-check:
	test -f migrations/000001_event_delivery_foundation.up.sql
	test -f migrations/000001_event_delivery_foundation.down.sql

migrate-up:
	go run ./cmd/migrate up --format json

migrate-status:
	go run ./cmd/migrate status --format json
