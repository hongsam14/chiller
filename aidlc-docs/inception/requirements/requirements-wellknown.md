# Requirements — Observation well-known-path GET provider + dictionary 심화 (r10)

**Increment**: r10 · **Branch**: (신규, off main) · **Date**: 2026-07-16
**Depth**: Standard · **Type**: Brownfield (scan 유닛 — observation + fan-out)
**Traces**: Backlog "🟠 HIGH — Observation well-known-path GETs 미배선" + "🟡 P1 dictionary/value fidelity" (배선 6개 항목 한정) · FR-03/FR-05

## 1. Intent
observation 시그니처는 URL-scoped라 **제출 URL의 응답만** 판정 → well-known 아티팩트를 요구하는 항목이 아티팩트를 못 받아 false-clean/N-A. 이 증분은 (1) approved 호스트에 well-known 경로를 GET해 소유 시그니처에 전달(배선)하고, (2) 그 6개 항목의 사전을 FN확장+FP축소로 심화한다.

## 2. Scope

### In scope
- **well-known GET provider (Q1=A: fan-out 합성)** — distinctHosts별로 아래 경로의 **URL-scoped WorkItem을 합성**해 fanOut에 추가. 기존 exec 파이프라인(fetch+approval gate+per-host rate+URL-scoped signatures)이 그대로 판정. 시그니처는 `Target.URL=well-known 경로`로 아티팩트를 받음.
  - PS-008 `/robots.txt` · PS-011 `/.env` · PS-010 `/.git/config` · PS-022 `/crossdomain.xml` · PS-012 backup 이름 목록 · PS-013 admin 경로 목록.
- **트리거(Q3=A)**: approved 호스트 기본 on, per-host 1회(dedup), approval gate(SR-3)·rate budget(SR-4) 준수.
- **MaxURLs 상호작용(Q6=A)**: 합성 well-known은 사용자 `MaxURLs`(SR-1) 카운트에서 **제외**, per-host rate는 공유.
- **dictionary 심화 (Q4=B + C1=A: 배선 6개 항목만, C2=A: FN확장+FP축소)**:
  - PS-011 `.env`: 키셋 확장(`JWT_SECRET`/`MAIL_PASSWORD`/`STRIPE_SECRET_KEY` 접두 포함) + `KEY=value` 형태/엔트로피 · **FP 가드**(프레임워크 `.env` doc 예시 오탐 방지).
  - PS-012 backup: 확장자 + backup-name heuristic(`index.php.bak`,`db.sql`,`www.zip`,`.orig/.7z/.gz/.tgz/.backup/.save`) · **FP 가드**(정상 아카이브 다운로드).
  - PS-010 `.git`: `config`+`HEAD`+directory-listing/`index`/`packed-refs` · **FP 가드**(`/.gitignore`·`/.github/` loose match).
  - PS-013 admin: 목록 확장(`/manager/html`,`/phpmyadmin`,`/dashboard`,`/backend`) · **FP 가드**(SPA 로그인 false "unauthenticated").
  - PS-008 robots: 키워드 확장(`/database`,`/sql`,`/.env`,`/logs`).
  - PS-022 crossdomain: 견고 — 추가 최소(`clientaccesspolicy.xml`은 배선 밖).
- **검증(Q5=A)**: r9 `test/fixtures/vuln-target/` dormant 표면 활성화(주석 제거) + in-image 통합 positive + FP/FN 코퍼스 유닛.

### Out of scope (이월)
- P1 **비-well-known** 항목 dictionary: PS-009 comment(토큰포맷/JSON) · PS-018 session `sid` · PS-023 captcha → 다음 증분(C1=A: "B는 다음 todo").
- **P0 transport**(br/zstd 미디코드·512KB 컷·`hasPasswordField` SPA) → 별개 correctness 증분(C3=A).
- `clientaccesspolicy.xml`(dead) · 능동 크롤링(FR-01 유지).

## 3. Functional Requirements
- **FR-W1**: fanOut이 distinct approved host별로 well-known 경로 집합의 **URL-scoped WorkItem을 합성·dedup**해 추가. 사용자 `MaxURLs` 카운트 제외.
- **FR-W2**: 합성 항목은 기존 URL-scoped exec 경로를 통과 — `Fetch`(approval gate+rate) → `ScanContext{Target.URL=well-known경로, Response}` → 소유 URL-scoped 시그니처 판정.
- **FR-W3**: well-known 아티팩트가 실제 취약(예: `.env`에 secret, `/listing` 아님)하면 소유 항목이 **real finding**; 존재하지 않으면(404 등) **checked-clean**(false-clean 아님).
- **FR-W4**: 6개 항목 사전 심화 — FN 확장 + FP 축소(위 §2). 각 항목은 여전히 **정확히 1 CWE** 소유(질문1 불변).
- **FR-W5**: 26 파티션·CWE 소유권·checker scope 불변(URL-scoped observation checker가 CWE 계속 소유). coverage 회계 의미 유지.

## 4. Non-Functional Requirements (enabled extensions)
- **security-baseline (blocking)**:
  - SEC-W1: well-known GET는 **approved 호스트에만**(기존 `fetch.gate.Authorize` 재사용, SR-3 fail-closed). 신규 outbound 우회 경로 없음.
  - SEC-W2: benign GET만(FR-03) — 능동 공격/퍼징 없음. 경로 목록은 소수·고정(무한 열거 아님).
- **resiliency-baseline (blocking)**:
  - RES-W1: 합성 fetch는 기존 Fetcher의 per-request timeout·ctx 취소·retry 예산·per-host rate를 그대로 상속(SR-4/SR-8). per-host 1회 sweep로 bounded — rate budget 고갈/행 없음.
  - RES-W2: well-known fetch 실패는 해당 경로만 영향(격리) — 정상 제출 URL 판정에 영향 없음.
- **property-based-testing (FULL, blocking)**:
  - PBT-W1: 심화된 시그니처 property — (a) 산출 CWE는 소유 CWE(1개), (b) FP 코퍼스(프레임워크 `.env` doc·정상 아카이브·`/.gitignore`·SPA 로그인)에서 **미발화**, (c) FN 코퍼스에서 발화. RE2라 ReDoS 불가(하드닝 불요).

## 5. Decisions (확정)
| # | 결정 | 근거 |
|---|---|---|
| Q1 | fan-out에서 well-known URL-scoped WorkItem 합성 | Q1=A |
| Q2 | 6개 항목 전부(robots/.env/.git/crossdomain/backup/admin) | Q2=A |
| Q3 | approved 호스트 기본 on, per-host 1회, gate+rate | Q3=A |
| Q4/C1 | 배선 + dictionary 심화(**배선 6개 항목만**; PS-009/018/023은 다음 todo) | Q4=B·C1=A |
| C2 | FN 확장 + FP 축소 둘 다, FP/FN 코퍼스 검증 | C2=A |
| Q5 | r9 fixture dormant 표면 활성화 + 통합 positive | Q5=A |
| Q6 | 합성 항목 MaxURLs 제외, per-host rate 공유 | Q6=A |
| C3 | P0 transport 미포함(별개 증분) | C3=A |

## 6. Acceptance Criteria
- r9 fixture 대상 스캔에서 **PS-008/011/012/022가 real finding**으로, PS-010/013이 아티팩트 존재 시 **judged**(N/A 아님)로 전환. dormant 주석 제거.
- FP 코퍼스: 프레임워크 `.env` doc·정상 아카이브·`/.gitignore`·SPA 로그인에서 **신규 FP 없음**.
- 합성 항목이 approved-host·rate budget·MaxURLs 제외 규율을 지킴(테스트로 단언).
- coverage 26 파티션·소유권·인터페이스 불변. 로컬 gates green + in-image 통합 positive green.

## 7. Open Questions
없음 (Q1-6 + C1-3 전부 해소).
