.PHONY: build test test-integration lint gosec secrets vuln sbom cover arch-check tidy dev-env ci signatures signatures-check subzy-db

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
# subzy takeover-fingerprint DB (#0e, r18). subzy v1.1.0 self-fetches this on every
# run from a dead URL → parse error → PS-025 tool-failed. It is subzy's OWN operational
# data, so we do NOT bundle it in the image: `make subzy-db` provisions it (pinned by
# commit) to a git-ignored path the operator mounts read-only at /opt/subzy/fingerprints.json (SUBZY_DB). The
# pin is the single source of truth for the version; refresh = bump SUBZY_DB_COMMIT.
SUBZY_DB_COMMIT  := 5bd4e12837911c8475486f1da922c9b9c706e632
SUBZY_DB_URL     := https://raw.githubusercontent.com/EdOverflow/can-i-take-over-xyz/$(SUBZY_DB_COMMIT)/fingerprints.json
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

# Regenerate the distilled signature config from the pinned third_party/ sources
# (Arch A / Level 2 signature-ingestion pipeline). Reproducible: same pinned
# sources → same configs/signatures/*.yaml.
signatures:
	go run ./tools/sigimport

# CI drift-check: the committed config MUST match regeneration from the pinned
# sources — catches a manual edit or a stale-after-source-bump config.
signatures-check: signatures
	@git diff --exit-code configs/signatures/ \
		|| { echo "configs/signatures drift — run 'make signatures' and commit"; exit 1; }

# Generates deploy/compose/.env with a fresh random evidence key. The repo ships no
# key material, not even a throwaway one (see deploy/compose/.env.example).
dev-env:
	@test ! -f deploy/compose/.env || { echo "deploy/compose/.env already exists; not overwriting"; exit 1; }
	@printf 'CHILLER_TOKEN=dev-token-%s\nCHILLER_EVIDENCE_KEY=%s\n' \
		"$$(openssl rand -hex 8)" "$$(openssl rand -base64 32)" > deploy/compose/.env
	@echo "wrote deploy/compose/.env (gitignored)"

# Provisions subzy's takeover-fingerprint DB (#0e) to a git-ignored path the operator
# mounts read-only at /opt/subzy/fingerprints.json (SUBZY_DB) (see deploy/compose/docker-compose.yml). Pinned
# by commit; `-f` fails loudly on a dead/moved URL (fixable by bumping SUBZY_DB_COMMIT),
# instead of subzy failing silently at scan time. Without this, PS-025 is honest
# tool-failed. Refresh: bump SUBZY_DB_COMMIT + re-run.
subzy-db:
	@mkdir -p deploy/subzy
	@echo "fetching subzy fingerprints @ $(SUBZY_DB_COMMIT)"
	@curl -fsSL "$(SUBZY_DB_URL)" -o deploy/subzy/fingerprints.json
	@test -s deploy/subzy/fingerprints.json || { echo "subzy DB empty — aborting"; rm -f deploy/subzy/fingerprints.json; exit 1; }
	@echo "wrote deploy/subzy/fingerprints.json (gitignored) from $(SUBZY_DB_URL)"

# Every blocking gate, in one command. This is what CI runs, split across jobs.
ci: build lint arch-check gosec test test-integration secrets
	@echo "all blocking gates passed"
