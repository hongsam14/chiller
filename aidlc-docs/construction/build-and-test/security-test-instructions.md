# Security Test Instructions — chiller

The `security-baseline` and `resiliency-baseline` extensions are **enabled (blocking)**. chiller is itself a security tool that sends outbound traffic, so its own safety invariants are load-bearing and must be tested, not just documented.

## 1. Dependency & supply-chain checks
```bash
go vet ./...                          # static checks (must be clean)
govulncheck ./...                     # known-vuln scan (install: go install golang.org/x/vuln/cmd/govulncheck@latest)
go mod verify                         # module checksums match go.sum
```
- **Expected**: vet clean; `govulncheck` reports no reachable vulnerabilities. Pin playwright-go to **v0.5200.1** (audited); reject v0.6100.0 (module-path mismatch).

## 2. Fail-closed authorization (ScanAPI, PR-7)
- **Invariant**: no scan is created/started without a valid Bearer token; an **empty configured token denies all** (misconfiguration ≠ open door).
- **Tests** (`internal/platform/api`): `TestPostScan_Unauthorized_FailClosed` (no/wrong token → 401, orchestrator never invoked), constant-time token compare (`crypto/subtle`).
```bash
go test -run 'Unauthorized|Authoriz' ./internal/platform/api/
```

## 3. Outbound scope containment (the core scanner-safety property, SR-3)
- **Invariant**: nothing goes outbound to an unapproved host — including via **redirect** (each hop is re-authorized) and via the **host-scoped probe path** (gated before the probe runs). Fail-closed.
- **Tests** (`internal/scan/safety`, `internal/scan/fetch`, `internal/scan/exec`):
  `TestFetcher_RedirectToUnapprovedHost_Blocked`, host-gate fail-closed in exec, approval ∩ allowlist intersection.
```bash
go test -run 'Approv|Redirect|Unapproved|FailClosed' ./internal/scan/...
```

## 4. Per-host rate budget (abuse prevention, SR-4)
- **Invariant**: no host is hit faster than its token-bucket budget; burst is clamped ≥1 so limiting can never spin.
- **Test**: `TestRateLimiter_*` (`internal/scan/safety`).
- **Retry budget (retryablehttp, DONE 2026-07-10)**: each retry attempt charges a per-host token — no N-retries/1-token bypass. Verified by `TestFetcher_RetriesTransientThenSucceeds_ChargesBudgetPerAttempt` (`internal/scan/fetch/retry_test.go`); the HTTP/2 fallback client is pointed at the same gated client so the redirect re-gate holds on every path.

## 5. Evidence & data-at-rest hygiene (FR-12/SEC-01/03)
- **Invariant**: response bodies are bounded (`io.LimitReader`, 512KB) and never persisted as findings; evidence snippets are bounded (`domain.MaxEvidenceSnippets`) and redacted before persistence.
- **Tests**: normalize/dedup evidence bounding (`findings/normalize`, `findings/dedup`); fetch body cap.

## 6. Financial auth-proxy integrity (PR-12)
- **Invariant**: a single authenticated `BrowserSession`; workers cannot fan out into multiple sessions (single-session invariant). The login keep-alive tab is excluded from the work pool T (no concurrency starvation, no extra sessions).
- **Tests** (`internal/platform/proxy`): session injection, work-cap == T with keep-alive reserved, ctx-cancel shutdown.
- **Deferred hardening (Infra)**: HTTPS CONNECT + bundled MITM CA issuance/trust, TLS interception hardening, storageState secret handling, session auto-refresh. Track as Infra security tasks.

## 7. Input validation (ingest, SR-1)
- **Invariant**: malformed/over-limit URL sets are rejected fail-closed (no partial scan); MaxURLs bounds fan-out; per-page JS discovery is capped (amplification guard).
```bash
go test -run 'Ingest|Discover|Cap' ./internal/scan/...
```

## Run the security-relevant subset
```bash
go test -race ./internal/scan/safety/... ./internal/scan/fetch/... ./internal/scan/exec/... \
              ./internal/platform/api/... ./internal/platform/proxy/...
```
**Expected**: all pass. These encode the blocking security-baseline requirements as executable checks.

---

# Dependency Vulnerability Scan (added 2026-07-11 — SECURITY-10)

## Run
```bash
make vuln
# = go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```
Wire this into CI **before** the image build. Exit status 3 = vulnerabilities affecting called code.

## Result of the 2026-07-11 run — ⚠️ 1 finding affecting our code
| ID | Package | Found in | Fixed in | Reached from |
|---|---|---|---|---|
| **GO-2026-5856** | Go stdlib `crypto/tls` (Encrypted Client Hello privacy leak) | `crypto/tls@go1.26.4` | **`go1.26.5`** | `proxy.Serve`, `fetch.Fetch`, `taskstore.Mongo` (tls.Dial), `workqueue.NewValkey` (tls.DialWithDialer) |

**Remediation**: bump the Go toolchain to **≥ 1.26.5** and rebuild. **No source change required.**
No third-party dependency on any call path is affected (15 further advisories exist in required modules but are unreachable from our code).

## Also verified (unit-proven)
- **Fail-closed authz** on `/scans` and `/api/introspect/queue` (missing/wrong Bearer → 401).
- **HTTP security headers on `/ui`** (SECURITY-04, newly applicable now that HTML is served): CSP, HSTS, `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Referrer-Policy`.
- **Secrets** are injected via env/K8s Secret — never in source, configs, or the image (SECURITY-12).

## SBOM
```bash
make sbom   # syft → sbom.cyclonedx.json (required for production images)
```

---

## r3 추가 (persistence 증분)

### 6. 봉투 암호화 (`internal/crypto`) — AES-256-GCM

| 속성 | 테스트 | 왜 |
|---|---|---|
| 암복호 항등 | `TestPBT_SealOpenRoundTrip` | 기본 |
| **AAD 불일치 시 반드시 실패** | `TestPBT_WrongAADAlwaysFails` | 봉투를 **다른 finding/URL로 옮겨 붙이는 것**을 차단. 캐시 오염이 미탐으로 이어지는 경로를 막는다 |
| **변조 검출** | `TestPBT_TamperedCiphertextFails` | AEAD — 복호화가 곧 무결성 검증 (SECURITY-13). 저장소 쓰기 권한을 얻은 자가 evidence를 조작하지 못한다 |
| **nonce 유일성** | `TestSeal_FreshNonceEachTime` (1000회) | GCM에서 nonce 재사용은 기밀성을 통째로 파괴한다 |
| **키 회전 정합** | `TestNewKeyring_MultiKeyRequiresExplicitIDs` | 위치 기반 id는 회전 시 **기존 봉투를 틀린 키로 열게** 만든다 → 키 2개 이상이면 id 필수 |
| **fail-closed** | `TestNewMongo_RequiresAKeyring` | 키 없이 durable 백엔드를 켜면 **기동 거부** (SECURITY-15) |

**키 관리**: `CHILLER_EVIDENCE_KEY` (base64 32B). K8s Secret/볼트 주입만. 회전은 `"k2:<new>,k1:<old>"`.
소스·이미지·매니페스트에 **하드코딩 금지** (compose의 dev 키는 로컬 전용이며 그렇게 명시돼 있다).

### 7. 데이터 잔존 (FR-12/NFR-16) — **이번 증분이 처음으로 실제 작동시킴**

- **회귀**: `TestFinalize_WipesCache` · `TestFail_WipesCache` · `TestStop_WipesCache` —
  세 종료 경로 각각에서 캐시 잔존 0을 단언. (증분 전에는 `WipeScan`이 **어디에서도 호출되지 않았다**)
- **in-flight 누수**: `TestFetcher_DoesNotCacheAfterTheScanWasCancelled` — stop 후 진행 중이던 fetch가 wipe된 캐시를 다시 채우지 못한다
- **백스톱**: 모든 캐시 키에 TTL(기본 6h) — 프로세스가 죽어 wipe를 못 해도 만료로 사라진다
- **evidence 사본**: 정상 종료 후 **1벌**(`findings` 컬렉션, 봉인). 원장 사본은 finalize에서 소멸 (`data-census.md` 규칙 O-2)

### 8. 남는 노출 (정직한 기록 — `data-census.md` §6)

- **URL은 평문**이다. 인덱스·재개·감사·귀속에 필요해 암호화할 수 없다. 쿼리스트링에 토큰이 실리면 노출된다 →
  TLS in-transit + 컬렉션/키 최소권한 + **로그 redaction**(R9)이 유일한 방어
- **워커 프로세스 메모리의 평문** — fetch와 regex 탐색에 불가피 (범위 밖)
- **실패·중단 스캔의 원장 evidence** — 재개를 위해 남지만 **봉인되어 있다**
