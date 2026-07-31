# Build and Test Summary — Evidence 강화 (r20)

**Stage**: Build and Test · **Branch**: feat/evidence-enrichment
**Date**: 2026-07-31 · **Status**: 로컬 게이트 전부 GREEN (approval pending)
**Scope**: `domain.Evidence{Snippets []string}` → 구조화 Evidence 교체 + kernel-panic 후
재개·검증 + workflow 코드리뷰 6건 수정.

## 1. 게이트 결과 (로컬)

| 게이트 | 명령 | 결과 |
|---|---|---|
| Build | `go build ./...` | ✅ |
| Build (integration) | `go build -tags integration ./...` | ✅ |
| Vet | `go vet ./...` | ✅ |
| Vet (integration) | `go vet -tags integration ./...` | ✅ |
| Format | `gofmt -l internal/ cmd/` | ✅ clean |
| Architecture | `make arch-check` | ✅ OK (data-plane↛platform, kernel↛unit) |
| Unit + PBT | `go test ./...` | ✅ 23 pkg ok, 0 FAIL |
| Race | `go test ./... -race` | ✅ no DATA RACE / FAIL |
| SAST | `make gosec` (pinned v2.22.9) | ✅ 0 issues |
| Secrets | `make secrets` (gitleaks v8.21.2) | ✅ no leaks |
| Coverage | `go test ./... -coverprofile` | 56.9% (module total) |

Integration 스위트(`//go:build integration`, testcontainers Mongo/Valkey)는 Docker
필요 → 기존 증분과 동일하게 **Docker 위임**(로컬은 컴파일만 검증, 위 integration 태그 build/vet green).

## 2. PBT (property-based testing, FULL — blocking)

- **경계**: dedup 후 `len(Items) ≤ MaxEvidenceItems`, snippet 길이 상한, 헤더 리스트/값 상한.
- **멱등**: `Dedup∘Dedup = Dedup`, 순서 무관 (P2 유지).
- **probe 날조 없음**: probe 경로 item의 Location/Request/Response = zero/nil.
- **seal 왕복 항등**: 임의 Evidence → `open(seal(e)) == e` (store `EvidenceAAD` +
  taskstore `LedgerEvidenceAAD` 각각).
- **강등 vs 손상 구분** (코드리뷰 F1/F2 회귀): 구 `[]string` 평문 → 빈 Evidence(에러 아님);
  구조체도 배열도 아닌 손상 평문 → **에러 표면화**(무언 소실 차단).
- **F1 회귀**: JS-소스 finding이 Request/Response=js 교환 + `Location.SourceURL=js.URL`.

## 3. 코드 리뷰 반영 (workflow high, 6건 CONFIRMED/PLAUSIBLE → 전부 수정)

플랜 A~I **누락 구현 없음** 확인(panic 절반적용 가능성 반증). dedup 병합/keepExistingEvidence
의심 4건은 검증서 refuted. 수정:

1. **taskstore_mongo.go** — resume 시 unmarshal 실패 evidence 무언 소실 → 구 스키마만 강등, 그 외 에러.
2. **store_mongo.go** — 동일 무언 소실 패턴 → 동일 수정.
3. **report_view.go** — `locationDTO.Offset` omitempty가 byte-0 매치 삭제 → omitempty 제거.
4. **contract_test.go** — no-alias 테스트 무의미화(SnippetStrings fresh slice) → Items 직접 변경.
5. **signatures.go** — content-type 중복(전용 필드+헤더) → allowlist에서 제외.
6. **report_ui.go** — 미정의 CSS `.f-evmeta`/`.f-evhyp` → style 규칙 추가.

신규 회귀 테스트: store/taskstore 각 "구 배열 강등" + "손상 에러" 케이스.

## 4. 불변식 (재확인)

26-파티션 · checker↔CWE 소유권 · probe 정직성(unwired→tool-failed) · 안전봉투 ·
제품 Dockerfile · platform seam 인터페이스 · **신규 의존성 0**.

## 5. 커밋

- `8f0f498` feat(evidence): structured Evidence schema for downstream AI (r20)
- `564285b` docs(aidlc): r20 evidence — requirements→code-gen + review

## 6. 재현 절차

```
go build ./... && go build -tags integration ./...
go vet ./... && go vet -tags integration ./...
gofmt -l internal/ cmd/           # empty = clean
make arch-check
go test ./...                     # 23 pkg ok
go test ./... -race               # no race
make gosec                        # 0
make secrets                      # no leaks
# integration (Docker 필요):
go test -tags integration ./...   # testcontainers Mongo/Valkey
```
