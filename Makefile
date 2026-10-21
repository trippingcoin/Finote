.PHONY: test test-integration test-web run

test-web:
	node --test internal/web/api_test.cjs

test:
	go test ./...
	go vet ./...

test-integration:
	@set -eu; trap 'docker compose -f compose.test.yaml down' EXIT; \
	 docker compose -f compose.test.yaml up --build --abort-on-container-exit --exit-code-from tests

run:
	docker compose up -d --build
