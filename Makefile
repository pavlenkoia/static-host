.PHONY: dev check check-fast

dev:
	@set -eu; \
	if [ ! -f .env ]; then echo 'Create .env from .env.examples and set STATIC_HOST_ADMIN_PASSWORD' >&2; exit 1; fi; \
	set -a; . ./.env; set +a; \
	npm --prefix web ci; \
	npm --prefix web run build; \
	exec go run ./cmd/static-host --base-domain ''

# Full local gate; CI runs the faster subset on every push and pull request.
check: check-fast
	npm --prefix web exec -- playwright install chromium
	npm --prefix web test

check-fast:
	npm --prefix web ci
	npm --prefix web run build
	go test -race -count=1 ./...
