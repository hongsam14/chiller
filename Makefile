.PHONY: build test test-integration lint gosec secrets vuln sbom cover arch-check tidy dev-env ci

MODULE := github.com/hongsam14/chiller

# --- Tool versions -----------------------------------------------------------
#
# Every tool is pinned (SECURITY-10: "pinned tool versions ... no `latest`").
# Nothing below needs to be pre-installed: Go tools run via `go run`, container
# tools via a digest-pinned image. That matters — .golangci.yml sat in this repo
# for weeks referenced by `make lint` and never once ran, because golangci-lint
# was not installed anywhere. A gate you cannot run is not a gate.
#
# .github/workflows/ci.yml calls these targets verbatim, so "green in CI" and
# "green on my machine" cannot drift apart.
GOLANGCI_VERSION := v2.6.0
GOSEC_VERSION    := v2.22.9
GOVULN_VERSION   := v1.6.0
GITLEAKS_IMAGE   := zricethezav/gitleaks:v8.21.2@sha256:0e99e8821643ea5b235718642b93bb32486af9c8162c8b8731f7cbdc951a7f46
SYFT_IMAGE       := anchore/syft:v1.33.0@sha256:f94e5d9fce1f2278491a8e3a63bd5f6ddb81fdfdbb8bf7a1637565c1d5344357

build:
	go build ./...

# -race: a data race is a correctness bug that only shows up under load, which is
# exactly when nobody is watching a test run.
test:
	go test -race ./...

# Durable-backend contract suites against real Mongo/Valkey containers (Q10=A).
# Requires Docker; excluded from `test` by the `integration` build tag.
#
# ./... , not ./internal/platform/... : the durable FindingsStore and ResponseCache
# live in the findings and scan units, and a platform-only target ran green while
# never touching them.
#
# -timeout 10m, not 30m: the suite takes ~15s. The CI job cap is 25 min, so a 10 min
# Go timeout fires FIRST and panics with a full goroutine dump — telling us *where*
# it hung, not merely *that* it hung. The last hang cost 25 minutes and had to be
# diagnosed by reading the code.
test-integration:
	go test -tags integration -timeout 10m ./...

lint:
	go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_VERSION) run ./...

# -exclude-dir=.claude: an untracked scratch worktree lives there. CI never sees it
# (git does not track it), but locally it would be scanned as a second copy.
gosec:
	go run github.com/securego/gosec/v2/cmd/gosec@$(GOSEC_VERSION) -exclude-dir=.claude -quiet ./...

# Secret scan over the FULL history, not the working tree: deleting a key from a
# file does not remove it from the commit that introduced it. There is no
# .gitleaksignore — the allowlist is empty on purpose, because "this particular
# secret is fine" reads exactly as plausible on the day it is not.
secrets:
	docker run --rm -v "$(CURDIR):/repo" $(GITLEAKS_IMAGE) detect --source=/repo --no-banner

# Dependency vulnerability scan (SECURITY-10). Reported, not blocking (C1=D): a CVE
# published this morning in the stdlib is not actionable by whoever is fixing a typo.
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULN_VERSION) ./...

# Software Bill of Materials (SECURITY-10), CycloneDX JSON.
sbom:
	docker run --rm -v "$(CURDIR):/src" -w /src $(SYFT_IMAGE) scan dir:. -o cyclonedx-json=sbom.cyclonedx.json

# Coverage is reported, never gated (Q7=A): a coverage threshold buys tests written
# to raise a number.
cover:
	go test -coverprofile=coverage.out ./... > /dev/null
	@go tool cover -func=coverage.out | tail -1

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

tidy:
	go mod tidy

# Generates deploy/compose/.env with a fresh random evidence key. The repo ships no
# key material, not even a throwaway one (see deploy/compose/.env.example).
dev-env:
	@test ! -f deploy/compose/.env || { echo "deploy/compose/.env already exists; not overwriting"; exit 1; }
	@printf 'CHILLER_TOKEN=dev-token-%s\nCHILLER_EVIDENCE_KEY=%s\n' \
		"$$(openssl rand -hex 8)" "$$(openssl rand -base64 32)" > deploy/compose/.env
	@echo "wrote deploy/compose/.env (gitignored)"

# Every blocking gate, in one command. This is what CI runs, split across jobs.
ci: build lint arch-check gosec test test-integration secrets
	@echo "all blocking gates passed"
