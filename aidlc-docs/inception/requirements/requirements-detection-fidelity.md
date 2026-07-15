# Requirements — 검출 정합성 확대: probe positive-finding 실측 + 통합 취약 fixture (r9, #0d 나머지)

**Increment**: r9 · **Branch**: `feat/probe-parser-fidelity` (현재) · **Date**: 2026-07-15
**Depth**: Standard · **Type**: Brownfield 후속 증분 (scan/probe 유닛 계속)
**Traces**: Backlog "🔴 TOP PRIORITY probe wiring" 잔여 · production-readiness #0d(partial) · execution-plan-probe §4b

## 1. Intent (의도 분석)
r8에서 humble/httpx/Corsy의 positive-finding 파서 스키마를 실 대상으로 검증·고정했다. 남은 **testssl(PS-002) · dirhunt(PS-004) · subzy(PS-025)** 는 벤치 대상(로컬 http)에 취약 표면이 없어 positive 스키마가 미검증이다. 이 증분의 목표는 세 파서를 **실제 취약 출력에서 올바른 CWE finding을 추출**하도록 실측·고정해 검출 정합성을 넓히고, 그 과정에서 사용자 제안대로 **재사용 가능한 로컬 통합 취약 fixture**를 만들어 외부 대상 의존을 줄이고 CI 재현 가능한 회귀 대상을 확보하는 것이다. 정직성 불변식(도구 미실행/미배선 → tool-failed)과 26 파티션은 유지한다.

## 2. Scope

### In scope
- **testssl.sh (PS-002 · CWE-1327 weak TLS)**: 외부 **badssl.com** 공개 취약 서브도메인으로 실 실행 → 실 `--jsonfile` 출력으로 파서/골든 확정 + 이미지 통합 positive 단언.
- **dirhunt (PS-004 · CWE-548 directory listing)**: 로컬 fixture의 nginx `autoindex on` 표면으로 실 실행 → 실 `--to-file` 리포트로 파서/골든 확정 + positive 단언.
- **subzy (PS-025 · subdomain takeover, NoCanonicalCWE)**: 로컬 fixture의 mock 지문 서버(GitHub Pages/S3 등 takeover fingerprint 응답)로 실 실행 → 실 `--output` JSON으로 파서/골든 확정 + positive 단언.
- **통합 취약 fixture** `test/fixtures/vuln-target/` (docker-compose, 레포 커밋 — C2=A):
  - **HTTP probe 5종 표면 로컬화** (C4=B): humble(보안헤더 누락: HSTS/X-Frame-Options/CSP 부재) · httpx(버전 배너 노출) · Corsy(permissive CORS: ACAO 반사) · dirhunt(autoindex) · subzy(takeover mock). r8 probe(humble/httpx/Corsy)도 이 hermetic fixture로 재검증(외부 Juice Shop/kblife 의존 제거).
  - **observation 취약 표면을 dormant로 미리 배치** (C5=B): PS-008 robots.txt · PS-010 `/.git/config` · PS-011 `/.env` · PS-012 backup 파일명 · PS-022 crossdomain.xml · PS-018 session-in-URL · PS-009 sensitive comments · 쿠키(Secure/HttpOnly/SameSite 없는 Set-Cookie). **현재는 finding 미발생**(well-known-path GET 미배선 — 별개 backlog) — 정직하게 문서화하고, provider 배선 증분에서 재사용.
- 실 골든 고정 + 유닛 positive 테스트 + 이미지 통합 positive 단언 (Q4=A, r8 정직성 규율).

### Out of scope (이월/별개)
- **testssl 로컬 weak-TLS 서버**: C4=B로 testssl은 외부 badssl 유지 → 로컬 weak-TLS 구성은 안 함.
- **observation well-known-path provider 배선**: 표면만 dormant로 심고, 실제 GET·판정 배선은 별개 backlog(🟠 HIGH) 증분.
- **changeme (PS-019)**: 여전히 미배선(F5) — 이번 스코프 아님.
- 시그니처 사전/값 정합성(P1), transport br/zstd·512KB(P0): 별개 backlog.

## 3. Functional Requirements
- **FR-D1**: testssl 파서는 실 badssl 출력에서 HIGH/CRITICAL TLS 취약을 CWE-1327 finding으로 추출한다(이미 로직 존재 → 실 스키마 대비 검증·필요 시 골든/파서 교정).
- **FR-D2**: dirhunt 파서는 실 autoindex 리포트에서 directory-listing 플래그를 CWE-548 finding으로 추출한다.
- **FR-D3**: subzy 파서는 실 mock-target 출력에서 `vulnerable=true` 엔트리를 subdomain-takeover finding(NoCanonicalCWE)으로 추출한다.
- **FR-D4**: 통합 fixture는 humble/httpx/Corsy positive 경로도 재현하여, 재검증 시 각 owned CWE(319/1021 · 693 · 346)를 그대로 산출한다.
- **FR-D5**: 각 파서의 실 골든을 `testdata/`에 고정하고, 유닛 테스트가 positive finding(올바른 CWE)과 clean/N/A(취약 표면 없을 때 empty)를 모두 단언한다.
- **FR-D6**: 이미지 통합 테스트(`//go:build integration`)가 대상이 확보된 probe에 대해 실 바이너리 실행→positive finding을 단언한다. 미확보분(있다면)은 정직하게 이월 표기.
- **FR-D7**: 정직성 불변식 유지 — 도구 미실행/미배선/malformed 출력 → tool-failed(no verdict), empty-clean 금지. 26 파티션 유지.

## 4. Non-Functional Requirements (enabled extensions)
- **security-baseline (blocking)**:
  - SEC-D1: fixture는 **의도적 취약 대상**이므로 반드시 `test/` 아래 테스트 전용, **제품 이미지/런타임에 미포함**(C3=A). compose는 격리 네트워크, 외부 노출 포트 없음/로컬 바인드.
  - SEC-D2: 제3자 무허가 능동 스캔 금지 — dirhunt/subzy는 로컬 fixture에만, testssl은 **공개 테스트 서비스(badssl.com)** 로 한정.
  - SEC-D3: fixture에 실 비밀 값 없음(`.env` 등 표면은 가짜/더미 값).
- **resiliency-baseline (blocking)**:
  - RES-D1: probe 서브프로세스 타임아웃·프로세스그룹 kill(NFR-P1) 회귀 없음. dirhunt는 기존 bounded argv(--exclude-sources/--max-depth/--timeout) 유지.
  - RES-D2: 외부 badssl 실측은 네트워크 의존 → 통합 테스트에서 실패 시 tool-failed로 정직 처리(회귀 gate를 flaky로 만들지 않음). golden 기반 유닛 테스트는 hermetic.
- **property-based-testing (FULL, blocking)**:
  - PBT-D1: 파서 불변식 — (a) 산출 finding의 CWE는 항상 해당 도구의 owned CWE 집합에 속한다, (b) malformed JSON → error(≠ empty-clean), (c) 취약 표면 없음 → empty. 값이 있는 경우 property 오라클로 검증.

## 5. Decisions (확정)
| # | 결정 | 근거 |
|---|---|---|
| Q1/C4 | testssl → 외부 **badssl.com** 실측 (로컬 weak-TLS 미구성) | Q1=A, C4=B |
| Q2 | dirhunt → 로컬 **nginx autoindex** fixture | Q2=A |
| Q3 | subzy → 로컬 **mock 지문** 서버 fixture | Q3=A |
| Q4 | 실 골든 + 이미지 positive 단언 (r8 정직성) | Q4=A |
| Q5 | 3개 모두 이번 증분(미확보분 정직 이월) | Q5=A |
| C1/C4 | **HTTP probe 5종**(humble/httpx/Corsy/dirhunt/subzy)을 하나의 로컬 fixture로 통합 | C1=B + 메모, C4=B |
| C2 | fixture를 `test/fixtures/vuln-target/`로 **레포 커밋**(재사용) | C2=A |
| C3 | fixture는 **스캔당하는 테스트 대상**일 뿐 — chiller 제품 코드에 취약/능동 로직 추가 안 함 | C3=A |
| C5 | observation 취약 표면도 fixture에 **dormant**로 배치(지금 finding 미발생, 문서화) | C5=B |

## 6. Acceptance Criteria
- testssl/dirhunt/subzy 각 파서가 실 취약 출력에서 올바른 CWE finding을 추출(유닛 골든 + 이미지 통합 positive).
- 통합 fixture(`test/fixtures/vuln-target/`)가 커밋되고, humble/httpx/Corsy/dirhunt/subzy positive 경로를 hermetic하게 재현.
- observation 표면이 fixture에 존재하되 현재 finding 미발생임이 테스트/문서로 정직하게 명시.
- 정직성 불변식·26 파티션 유지, 로컬 gates(build/vet/gofmt/arch-check/race/gosec) green, 이미지 통합 positive green.
- badssl 네트워크 의존은 통합 테스트에서 실패해도 tool-failed로 정직 처리(회귀 gate flaky 방지).

## 7. Open Questions
없음 (Q1-5 + C1-5 전부 해소).
