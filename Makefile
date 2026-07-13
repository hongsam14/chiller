.PHONY: build test test-integration lint arch-check tidy vuln sbom

MODULE := github.com/enki-offen/chiller

build:
	go build ./...

test:
	go test ./...

# Durable-backend contract suites against real Mongo/Valkey containers
# (Q10=A). Requires Docker; excluded from the default `test` target by the
# `integration` build tag.
#
# ./... , not ./internal/platform/... : the durable FindingsStore and ResponseCache
# live in the findings and scan units, and a platform-only target would have run
# green while never touching them.
#
# Containers are started per suite by testcontainers, so this is slow (minutes).
test-integration:
	go test -tags integration -timeout 30m ./...

lint:
	golangci-lint run

tidy:
	go mod tidy

# Dependency vulnerability scan (SECURITY-10). Run in CI before building images.
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@latest ./...

# Software Bill of Materials for the production image (SECURITY-10). Requires
# syft; emits CycloneDX JSON.
sbom:
	syft packages dir:. -o cyclonedx-json=sbom.cyclonedx.json

# NFR-02 import guard (works without golangci-lint installed).
# Fails if the data plane imports the control plane, or the domain kernel
# imports any unit. See unit-of-work-dependency.md §6.
arch-check:
	@echo "arch-check: data plane must not import platform"
	@! go list -deps ./internal/scan/... ./internal/findings/... 2>/dev/null \
		| grep -q '$(MODULE)/internal/platform' \
		|| { echo "NFR-02 violation: data plane imports platform"; exit 1; }
	@echo "arch-check: domain kernel must import no unit"
	@! go list -deps ./internal/domain/... 2>/dev/null \
		| grep -Eq '$(MODULE)/internal/(platform|scan|findings)' \
		|| { echo "NFR-02 violation: domain imports a unit"; exit 1; }
	@echo "arch-check: OK"
