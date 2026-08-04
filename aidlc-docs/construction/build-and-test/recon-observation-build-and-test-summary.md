# Build and Test Summary — recon 노하우 → observation (r21)

**Branch**: `feat/recon-observation`. 커밋 `5456a0c`(feat)·`7eeb0c5`(docs). **로컬 전 게이트 GREEN.**

## 로컬 게이트 (committed tree)
| 게이트 | 명령 | 결과 |
|---|---|---|
| build | `go build ./...` | ✅ |
| build (integration) | `go build -tags integration ./...` | ✅ |
| vet | `go vet ./...` | ✅ |
| vet (integration) | `go vet -tags integration ./...` | ✅ |
| gofmt | `gofmt -l internal/ tools/ test/` | ✅ clean |
| arch-check | `make arch-check` | ✅ (data plane ⊥ platform, domain 무-import) |
| unit + PBT (-race) | `go test ./... -race` | ✅ 0 FAIL, 0 DATA RACE |
| gosec (pinned v2.22.9) | `make gosec` | ✅ 0 |
| secrets (gitleaks full-history) | `make secrets` | ✅ no leaks (84 commits; 신규 third_party/·configs/signatures/ allowlist 적용) |
| signatures-check (drift) | `make signatures-check` | ✅ 재생성 멱등, drift 0 |
| lint (golangci v2.6.0) | `make lint` | ✅ 0 issues |
| coverage (observation) | `go test -cover` | 85.8% |

**integration 스위트**(testcontainers Mongo/Valkey, `//go:build integration`)는 Docker 위임 — 로컬은 integration 태그 **컴파일**만 검증. probe-integration·supply-chain은 CI Docker에서 실행.

## 검증 커버리지 (r21 신규/강화)
- **FP/FN 코퍼스**(`recon_observation_test.go`): 백업 매직(zip/sqlite/sqldump 양성 · report.zip/soft-404 음성)·.DS_Store(Bud1 양성 · HTML 음성)·제품노출(actuator env/phpinfo 양성 · soft-404 음성)·스택트레이스(python/java/whoops 양성 · 벤ign error prose 음성)·에러정보(django urlconf 양성 · 404 음성)·PII(주민번호/email/phone/**compound userEmail·mobilePhone·emailAddress** 양성 · 랜덤digit/**bad-month 202401**/userType/**telemetry** 음성).
- **PBT**: PII 원문값 미노출(**콜론-포함 값 회귀 포함**)·offset 원문 재로케이트·body offset 경계.
- **파티션 회계**(coverage-wiring·e2e): PS-005/006 owned(Checked)·PS-014/026 delegated·CWE-359 26-미포함·15 observation judged/2 delegated/9 probe·boot-totality(KISA+FSEC).

## 코드 리뷰 반영
workflow high 리뷰 10건 → 8 수정(maskPII 누출·resident-id 오탐·compound-key 미탐·symfony 헤더 죽은규칙·/env 오탐·죽은 probe·FindAllIndex·복붙) + 2 문서화 trade(stackTrace 게이트·PS-005/006 수동승격). 회귀 테스트 추가.

## 불변 (재확인)
26-파티션 items 목록·checker↔CWE 소유권(one-CWE-one-owner)·boot-totality(신규 CWE 209/497/359 매핑, CWE-359 Project=[])·FR-6 소유-only(신규 well-known)·OSS-증류(손수 금지)·RE2·**신규 Go dep 0**·probe(9)·제품 Dockerfile·platform seam 불변.

## 이월 (정직)
- H2 콘솔·범용 apache server-info: pin `9419e84`에 nuclei 템플릿 부재 → 증류 제외.
- stackTrace 코드-전시 페이지 오탐(수동탐지 본질적 한계)·PS-005/006 능동트리거 상실(Q3=B 의도) — 문서화 수용.
- integration/probe-integration/trivy/syft는 CI Docker 실행 필요(로컬 미실행).
