.PHONY: test test-integration

# Run unit tests. Integration tests skip without FM_TEST_* env vars.
test:
	go test ./...

# Run integration tests against a live FileMaker server.
# Loads credentials from .env in the repo root (gitignored).
test-integration:
	@test -f .env || { echo "missing .env (see README: Running integration tests)"; exit 1; }
	set -a; . ./.env; set +a; go test -v ./...
