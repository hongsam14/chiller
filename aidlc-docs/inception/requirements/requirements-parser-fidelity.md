# Requirements — Probe 파서 실측 (#0d)

## Intent Analysis
- **User request**: 배선된 probe 파서를 **실제 취약 대상 출력**으로 검증·재작성. 지금은 툴이 실행·checked까지만 증명됐고 positive-finding 추출은 미검증 — 특히 **humble 파서는 실 섹션기반 스키마와 달라 finding을 0개** 낸다.
- **Request type**: Enhancement/Bug-fix (brownfield) — 기존 `parsers.go`의 파서 로직을 실측으로 교정. 신규 컴포넌트·인터페이스 변경 없음.
- **Scope estimate**: Single component (`internal/scan/checking/probe/parsers.go` + `testdata/` + 테스트).
- **Complexity**: Moderate — 실 대상 출력 캡처 + 스키마 정확 매핑.

## 결정 (답변)
| Q | 답 | 값 |
|---|---|---|
| Q1/C1·C2 | 대상 | **OWASP Juice Shop, 로컬 docker `http://localhost:3000`** (내가 띄움). 외부 취약 사이트는 이번 제외(이월). |
| Q2 | 범위 | **A — humble + httpx 우선**. Juice Shop이 실제로 갖는 것에 한해 Corsy(permissive CORS)도 기회적으로 포함 가능. |
| Q3 | subzy | 이월(대상 없음) — 스펙 기반 유지, 미실측 명기. |
| Q4 | 골든 | **A — 실 출력을 `testdata/*.json` 골든으로 커밋** + 유닛 테스트 golden→finding. |

## 대상 커버리지 (Juice Shop 로컬 http)
| 툴 | PS | Juice Shop에서 실측 가능? |
|---|---|---|
| **humble** | 001/015/016/017/024 | ✅ 보안 헤더 누락 다수 |
| **httpx** | 007/020 | ✅ tech/server 배너(Express/Node) |
| **Corsy** | 021 | ⚠️ Juice Shop은 permissive CORS 보유 → 기회적 실측 |
| testssl | 002 | ❌ 로컬 http(약한 TLS 없음) → 이월 |
| dirhunt | 004 | ❌ dir 리스팅 없음 → 이월 |
| subzy | 025 | ❌ dangling 서브도메인 없음 → 이월(스펙 기반) |

## Functional Requirements
- **FR-D1 (실 캡처)**: Juice Shop을 로컬 docker로 띄우고, humble·httpx(+가능 시 Corsy)의 **실제 JSON 출력을 캡처**해 `testdata/*.json` 골든으로 커밋(Q4=A). 캡처 절차를 요약에 기록.
- **FR-D2 (humble 파서 재작성)**: humble의 **실 섹션기반 스키마**(`"[1. Missing HTTP Security Headers]"` 등)를 파싱해 누락 보안헤더/쿠키 취약을 소유 CWE(319/614/1004/1275/1021)로 매핑. 현재 `{missing,cookies}` 추측 스키마 폐기.
- **FR-D3 (httpx 파서 검증/정제)**: 실 httpx `-json` 출력에서 버전 노출을 정확히 판정(CWE-693). 오탐/미탐을 실 출력 기준으로 조정.
- **FR-D4 (positive 회귀)**: 각 파서 유닛 테스트가 **실 골든 → 기대 finding(≥1)**을 단언. humble은 실제로 finding을 내야 함(현 0개 → 수정 증명).
- **FR-D5 (통합 positive)**: `//go:build integration` 테스트가 실행 중인 Juice Shop에 붙어 humble·httpx가 **실제 finding을 산출**함을 확인(이미지 내부, 실 바이너리). 기존 "clean이면 통과"를 "positive면 통과"로 강화.
- **FR-D6 (정직성 불변)**: 파싱 실패는 error(tool-failed), 빈 출력은 clean — 불변 유지. 이월 툴(testssl/dirhunt/subzy)의 파서는 "스펙 기반·미실측"으로 명기, 회귀 없음.

## Non-Functional Requirements
- **NFR-D1 (결정론)**: 골든은 커밋된 실 출력 → 테스트 결정론적. Juice Shop 버전 핀(이미지 태그) 기록.
- **NFR-D2 (evidence 경계)**: finding evidence ≤ `domain.MaxEvidenceSnippets`; 소유 CWE만.
- **NFR-D3 (PBT)**: 해당 시 속성 테스트 — 어떤 유효 humble/httpx JSON에도 finding의 CWE는 그 툴 소유 집합의 부분집합, Severity Valid.
- **NFR-D4 (정직 문서화)**: testssl/dirhunt/subzy는 이번 미실측임을 요약·production-readiness #0d에 명기(부분 종결).

## Out of Scope
- testssl/dirhunt/subzy 실측(대상 없음) · 외부 취약 사이트 · 시그니처(observation) 품질 · §2/§4.

## Key Summary
Juice Shop(로컬 docker)으로 **humble·httpx(+가능 시 Corsy)** 파서를 실측 재작성한다. 최우선은 **humble 파서를 실 섹션기반 스키마로 고쳐 실제 finding을 내게** 하는 것(현재 0개). 실 출력을 골든으로 고정(Q4=A)하고 통합 테스트로 positive를 증명. testssl/dirhunt/subzy는 대상 확보 시 다음 증분으로 정직히 이월.
