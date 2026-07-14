# Infrastructure Design Plan — unit `ci`

**Created**: 2026-07-13
**Requirements**: `inception/requirements/requirements.md` §12 (r6, approved)
**Execution plan**: `inception/plans/execution-plan-ci.md` (approved)

**Scope note**: For this unit, "infrastructure" is **the pipeline itself**. There is no cloud compute/storage/messaging/networking to map — the runtime infrastructure (Mongo, Valkey, k8s, proxy) was designed in the `platform` and `persistence` increments and is **not touched here** (`shared-infrastructure.md` unchanged). The categories the rules ask about are therefore evaluated as follows:

| Category | Applicability to unit `ci` | Evidence |
|---|---|---|
| **Deployment Environment** | **Applicable** → the CI execution environment: GitHub Actions, cloud-hosted runners (Q1=A, Q2=A). No cloud provider choice — CI only, no CD (Q12=A). | requirements §12.2 D-1, D-2, D-14 |
| **Compute Infrastructure** | **Applicable** → runner sizing/parallelism: the job graph and what runs concurrently (CI-NFR-03). | CI-NFR-03 |
| **Storage Infrastructure** | **Applicable (narrow)** → the Go module/build **cache** on the runner, and the tension between cache speed and the clean-runner guarantee (CI-NFR-04). No database decisions. | CI-NFR-04 |
| **Messaging Infrastructure** | **N/A** | No queues, no events. The integration job *starts* Mongo/Valkey containers, but those are test fixtures, not infrastructure this unit designs. |
| **Networking Infrastructure** | **N/A** | No load balancer, no gateway, no topology. The runner needs egress to fetch tools; nothing to design. |
| **Monitoring Infrastructure** | **Applicable (narrow)** → what the pipeline *reports* (coverage, govulncheck, trivy, SBOM) versus what it *blocks* on. There is no runtime observability change (OTel remains infra-plan #3, deferred). | D-6, D-7, D-12, CI-FR-05..07 |
| **Shared Infrastructure** | **Applicable** → `Makefile` is the shared contract between "what CI runs" and "what a developer runs"; drift between them is the failure mode this unit must prevent. | §12.5 X-1 |

---

## Facts established before designing (measured, not assumed)

1. **Go 1.26.5 is installed and verified** (`/usr/local/go`, official tarball, SHA256 matched). `go build`/`go vet` OK, **`go test ./...` 25 pkg all ok / FAIL 0**, **`govulncheck` = 0 vulnerabilities affecting code → GO-2026-5856 is gone.** Risk R-1 is closed.
2. **A remote now exists**: `origin` = `git@github.com:hongsam14/Service-Passive-Scanner.git` (private). Default branch `main` (local `master` renamed to match). Risk R-3 is closed — an Actions run can actually be observed.
3. **`.golangci.yml` is v1 schema and golangci-lint v2 refuses to load it.** Migrated-to-v2 trial run: **8 findings** (errcheck 7, staticcheck 1), **depguard 0**.
4. **gosec: 6 real findings** — G112 ×2 (`ReadHeaderTimeout` missing), G304 ×3 (config loaders), G115 ×1 (`retry.go:86`).
5. **gitleaks over full history: 1 finding.** `CHILLER_EVIDENCE_KEY` in `deploy/compose/docker-compose.yml:43`, committed in **`a3ad705`**. The value base64-decodes to **`dev-only-key-do-not-use-in-prd!!`** — a deliberate dev placeholder, flagged by entropy (4.59). **It is a false positive in substance and a true positive in form.** Nearby, `CHILLER_TOKEN: "dev-token-change-me"` is also a hardcoded credential literal (not flagged — entropy too low).

### Why fact 5 forces a decision *now*

`gitleaks detect` scans **git history**, not the working tree. **Deleting the line today does not make the gate pass** — commit `a3ad705` still contains it. The available moves are:
- rewrite history (still cheap: **11 commits, never pushed** — this is the last moment it costs nothing), or
- allowlist the fingerprint, or
- scan only new commits instead of full history (which means a secret committed *before* CI existed is never detected — a permanently blind spot).

D-10 made gitleaks **blocking**. So this is not a stylistic preference: it determines whether the pipeline is red on day one.

---

## Design steps

- [x] **S1** — Job graph: decide parallel vs serial layout; write it as a diagram (Mermaid + text alternative)
- [x] **S2** — Tool version pinning table (exact versions + Action commit SHAs) — CI-NFR-01
- [x] **S3** — Toolchain strategy: how Go 1.26.5 is obtained on the runner and locally; behaviour under `GOTOOLCHAIN=local`
- [x] **S4** — Base-image digest resolution: actual `sha256:` values for the build and runtime bases + refresh policy
- [x] **S5** — Blocking vs reporting matrix: map D-6/D-8/D-10/D-11/D-12 onto concrete job/step exit-code behaviour
- [x] **S6** — Cache strategy and its security boundary (CI-NFR-04)
- [x] **S7** — Secret-handling design: resolve the gitleaks history finding (Q1) and the compose-file credential literals (SECURITY-12)
- [x] **S8** — HTTP server hardening design: `ReadHeaderTimeout` and any sibling timeouts, with values justified against SR-8 (Q2)
- [x] **S9** — Disposition of gosec G304/G115 (Q3, Q4)
- [x] **S10** — `Makefile` ↔ workflow contract: guarantee no drift between CI commands and developer commands
- [x] **S11** — Local verification protocol: exactly which gates are provable locally, and what only a real Actions run can prove
- [x] **S12** — Write `infrastructure-design.md` + `deployment-architecture.md` (pipeline topology) + compliance summary

---

# Questions

Answer with a letter after each `[Answer]:` tag. `X) 기타`를 고르면 뒤에 직접 설명해 주세요.

## Question 1 — gitleaks 히스토리 탐지 처리 (가장 중요)

`deploy/compose/docker-compose.yml:43`의 `CHILLER_EVIDENCE_KEY`가 커밋 `a3ad705` 히스토리에 있습니다. 값은 `dev-only-key-do-not-use-in-prd!!`(개발용 자리표시자). D-10에 따라 gitleaks는 **blocking**이므로, 아무것도 안 하면 **CI는 첫날부터 빨간불**입니다.

**아직 push하지 않았습니다 — 히스토리 재작성이 공짜인 마지막 순간입니다** (11커밋, 원격 없음).

A) **compose에서 리터럴 제거 + 히스토리 재작성** — `CHILLER_EVIDENCE_KEY`/`CHILLER_TOKEN`을 `.env`(gitignore)로 옮기고 compose는 `${VAR}`로 참조, `.env.example`에 자리표시자 제공. 그 뒤 `git filter-repo`로 히스토리에서 값 제거. **SECURITY-12("IaC 템플릿에 자격증명 금지")를 문자 그대로 충족하고, allowlist 없이 gitleaks가 깨끗하게 통과합니다.** 비용: 히스토리 재작성(지금은 공짜, push 후엔 아님)

B) **compose에서 리터럴 제거 + 히스토리는 그대로 두고 fingerprint allowlist** — `.gitleaksignore`에 `a3ad705…:43` 한 줄 + 사유 주석. 앞으로의 신규 시크릿은 잡히고, 과거 1건만 면제. 히스토리 재작성 없음. 다만 **"우리 히스토리에 dev 키가 영구히 남아 있다"는 사실은 유지**됩니다(그 자체는 무해)

C) **리터럴 유지 + allowlist만** — compose는 그대로(로컬 dev 편의 유지), `.gitleaksignore`로 면제. SECURITY-12의 문자에는 어긋나지만 실질 위험은 0

D) **gitleaks를 diff-only 모드로** — 새 커밋만 스캔(`gitleaks protect`). 과거는 아예 안 봄. **CI 도입 이전에 커밋된 진짜 시크릿은 영원히 발견되지 않는 사각지대가 생깁니다.** 권장하지 않음

X) 기타

[Answer]: A

## Question 2 — HTTP 서버 타임아웃 (X-2, gosec G112)

두 서버에 타임아웃이 **하나도** 없습니다:
- `internal/platform/proxy/authproxy.go:46` — 브라우저 세션용 **포워드 프록시**
- `cmd/chiller/main.go:239` — **ScanAPI** (제어 평면)

참고: 스캔 측 요청 데드라인은 `internal/scan/fetch/fetch.go:19`의 **20초**(SR-8, NFR-12)입니다.

A) **`ReadHeaderTimeout`만 추가** (양쪽 10초) — G112를 닫는 **최소** 변경. Slowloris는 헤더를 천천히 보내는 공격이므로 이것만으로 방어됩니다. `ReadTimeout`/`WriteTimeout`을 안 걸므로 **긴 응답·큰 바디를 끊을 위험이 없습니다** (프록시는 임의 크기의 응답을 중계합니다). **(권장 — 위험 대비 이득이 가장 좋음)**

B) **`ReadHeaderTimeout` + `IdleTimeout`** (10초 / 120초) — 위에 더해 유휴 keep-alive 연결 정리

C) **전체 타임아웃 세트** (`ReadHeaderTimeout` 10s + `ReadTimeout` 30s + `WriteTimeout` 60s + `IdleTimeout` 120s) — 가장 엄격. **주의: 프록시에 `WriteTimeout`을 걸면 큰 응답(대용량 JS 번들 등) 중계가 중간에 끊길 수 있습니다.** 이 서비스가 하는 일이 정확히 그것입니다

X) 기타 (값을 직접 지정)

[Answer]: A

## Question 3 — gosec G304 (설정 파일 로더 3곳)

`internal/findings/config/{spec,severity,mapping}.go`가 `os.Open(path)`로 **변수 경로**를 엽니다. 경로는 운영자가 플래그로 주는 값이고 사용자 입력이 아닙니다.

A) **`#nosec G304` + 사유 주석** — "운영자가 지정한 설정 경로이며 신뢰 경계 안" 이라고 명시. 코드 변경 없음, 판단 근거가 소스에 남음 **(권장)**

B) **경로 검증 추가** — 설정 디렉터리 하위로 제한(`filepath.Clean` + prefix 검사). 방어는 늘지만, **운영자가 임의 경로에 설정을 두는 정당한 사용을 막습니다**

C) **gosec 설정에서 G304 규칙 자체를 비활성화** — 앞으로 **진짜** 경로 주입이 생겨도 못 잡습니다. 권장하지 않음

X) 기타

[Answer]: A

## Question 4 — gosec G115 (`retry.go:86` int→uint 오버플로)

```go
d := r.base << uint(2*(attempt-1))   // 1x, 4x, 16x
```
호출부(`retry.go:55-58`)를 확인한 결과 **`attempt > 0`일 때만 `sleep`이 호출**되므로 현재는 오버플로가 **발생하지 않습니다**. 즉 실제 버그는 아닙니다. 다만 `sleep`을 `attempt=0`으로 부르는 코드가 나중에 추가되면 `uint(-2)`가 되어 시프트량이 거대해지고 **백오프가 조용히 0이 됩니다**(재시도 폭주). 방어가 호출부에만 있고 함수 자체에는 없는 구조입니다.

A) **함수 안에 가드 추가** — `if attempt < 1 { attempt = 1 }`. 방어를 호출부가 아니라 함수로 옮김. gosec도 만족 **(권장)**

B) **`#nosec G115` + 사유 주석** — "호출부가 attempt≥1을 보장" 이라고 명시. 코드 동작 불변이지만 **보장이 여전히 원격에 있습니다**

C) **시프트 대신 곱셈으로 재작성** — `d := r.base * time.Duration(1<<(2*(attempt-1)))` (여전히 같은 문제). 실익 없음

X) 기타

[Answer]: A

## Question 5 — Go 모듈/빌드 캐시

캐시를 쓰면 CI가 훨씬 빨라지지만, CI-NFR-04("clean runner에서 재현 가능")와 긴장 관계입니다.

A) **`actions/setup-go`의 내장 캐시 사용** (`go.sum` 해시를 키로) — 표준적이고, 키가 `go.sum`에 묶여 있어 의존성이 바뀌면 자동 무효화됩니다. **(권장)**

B) **캐시 없음** — 매 실행이 완전한 clean. 느리지만(매번 모듈 재다운로드) 재현성이 가장 강함

C) **캐시 사용하되 `main` push에서는 캐시 무시하고 clean 빌드** — 절충

X) 기타

[Answer]: A + C

## Question 6 — 잡 그래프 (CI-NFR-03: 빠른 게이트를 느린 잡 뒤에 줄 세우지 말 것)

A) **3개 잡 병렬** — `fast`(fmt/vet/arch-check/lint/build/unit test -race/gosec/gitleaks) · `integration`(Docker, 컨테이너) · `image-scan`(이미지 빌드 + trivy). 셋이 동시에 시작. 빠른 잡은 몇 분 안에 피드백, 통합 잡은 자기 속도로. **(권장)**

B) **2개 잡** — `fast` + `integration`(이미지 빌드/스캔을 integration에 합침). 잡 수가 적어 단순하지만 이미지 스캔이 통합 테스트를 기다림

C) **단일 잡 순차 실행** — 가장 단순. **CI-NFR-03 위반** — 컴파일 에러를 30분 뒤에 알게 됩니다

X) 기타

[Answer]: A

## Question 7 — 같은 PR에 새 커밋이 오면 이전 실행을 취소할까요?

A) **취소함** (`concurrency.cancel-in-progress: true`) — 러너 시간 절약, 최신 커밋 결과만 유효. **(권장)**

B) **취소 안 함** — 모든 커밋의 결과가 남음. 러너 시간을 더 씀

X) 기타

[Answer]: A

## Question 8 — 통합 잡 타임아웃

`make test-integration`은 `-timeout 30m`으로 설정돼 있습니다. 직전 실측에서 실패가 **25분 행(hang)으로 위장된 적이 있습니다**(계약 스위트가 `Consume`을 안 켠 버그).

A) **잡 타임아웃 25분** (go 자체 타임아웃 30분보다 **짧게**) — 행이 발생하면 잡이 먼저 죽어 빠르게 빨간불. Go 타임아웃은 스택 덤프용 백스톱

B) **잡 타임아웃 45분** (go 30분보다 길게) — Go의 타임아웃이 먼저 걸려 **패닉 스택 덤프**를 남김(디버깅에 유용). 대신 행이면 30분을 기다림 **(권장 — 행의 원인을 알 수 있는 게 더 중요)**

C) **기본값(6시간)** — 행이 나면 러너를 오래 붙잡음. 권장하지 않음

X) 기타

[Answer]: A

## Question 9 — 베이스 이미지 다이제스트 갱신 정책

다이제스트로 고정하면(D-13) 베이스 이미지 보안 패치가 나와도 **자동으로 따라가지 않습니다** — 누군가 갱신해야 합니다.

A) **Dependabot 활성화** (`.github/dependabot.yml`) — Docker 베이스 이미지 + GitHub Actions 버전을 주기적으로 PR로 올려줌. **고정의 이점을 유지하면서 방치를 막습니다.** GitHub 기본 기능, 추가 비용 0 **(권장)**

B) **수동 갱신** — 사람이 기억해서 갱신. 현실적으로는 **아무도 안 하게 됩니다** (nightly도 없으므로 알려줄 주체도 없음)

C) **Renovate** — 더 강력하지만 앱 설치 필요

X) 기타

[Answer]: A

## Question 10 — Trivy 리포트 범위 (D-12로 non-blocking 확정)

A) **HIGH/CRITICAL만 리포트** — 노이즈 적음 **(권장)**

B) **전부 리포트** (LOW/MEDIUM 포함) — 정보량 많지만 distroless라도 목록이 길어질 수 있음

X) 기타

[Answer]: A

---

**답변 후 채팅에 "완료"** 라고 알려주세요. 모순을 검토한 뒤 설계 아티팩트를 작성합니다.
