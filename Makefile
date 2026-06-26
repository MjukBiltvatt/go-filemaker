# go-filemaker
#
# `make test`        - hermetic unit tests (httptest mocks); no server needed.
# `make integration` - integration tests against a real FileMaker Server.
#                      Reads credentials from .env (FM_HOST, FM_DATABASE,
#                      FM_USERNAME, FM_PASSWORD, FM_LAYOUT). See integration_test.go.
#
#                      Narrow to specific tests with RUN (a -run regexp):
#                        make integration RUN=TestIntegrationDateTime
#                      Toggle request/response logging inline with FM_DEBUG:
#                        make integration FM_DEBUG=1 RUN=TestIntegrationCRUD
# `make test-all`    - unit tests, then the full integration suite.

.PHONY: test integration test-all vet

# -run regexp for the integration suite; defaults to every TestIntegration*.
RUN ?= Integration

test:
	go test ./...

vet:
	go vet ./...

# Unit tests first (fast, no server), then integration. Running them in this
# order surfaces hermetic failures before the suite touches the host, and a unit
# failure stops the run before integration starts.
test-all: test integration

# Loads .env into the environment, then runs the integration-tagged suite.
# Tests skip (rather than fail) when the required FM_* variables are absent.
# -count=1 disables Go's test cache: results depend on the live host and on the
# FM_* environment, neither of which is part of the cache key, so without it a
# stale cached run (e.g. a prior all-skipped run) would be replayed.
integration:
	@set -a; [ -f .env ] && . ./.env; set +a; \
		go test -tags=integration -run '$(RUN)' -v -count=1 ./...
