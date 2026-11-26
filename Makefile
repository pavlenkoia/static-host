.PHONY: dev check check-fast check-docs

dev:
	@set -eu; \
	if [ ! -f .env ]; then echo 'Create .env from .env.examples and set STATIC_HOST_ADMIN_PASSWORD' >&2; exit 1; fi; \
	set -a; . ./.env; set +a; \
	npm --prefix web ci; \
	npm --prefix web run build; \
	exec go run ./cmd/static-host --base-domain ''

# Full local gate; CI runs the faster subset on pull requests.
check: check-fast
	npm --prefix web exec -- playwright install chromium
	npm --prefix web test

check-fast:
	npm --prefix web ci
	npm --prefix web run build
	go test -race -count=1 ./...

check-docs:
	python3 scripts/check-docs.py
	okf lint .llm-wiki --strict-frontmatter
