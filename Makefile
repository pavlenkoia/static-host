.PHONY: check

# One entry point for local development and CI; stop on the first failure.
check:
	npm --prefix web ci
	npm --prefix web exec -- playwright install chromium
	npm --prefix web run build
	go test -race -count=1 ./...
	npm --prefix web test
