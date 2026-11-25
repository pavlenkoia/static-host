.PHONY: check check-fast

# Full local gate; CI runs the faster subset on every push and pull request.
check: check-fast
	npm --prefix web exec -- playwright install chromium
	npm --prefix web test

check-fast:
	npm --prefix web ci
	npm --prefix web run build
	go test -race -count=1 ./...
