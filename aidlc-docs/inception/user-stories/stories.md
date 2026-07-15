# User Stories — chiller (passive-scanner engine)

**Stage**: INCEPTION → User Stories (Part 2) · **Date**: 2026-07-08
**Spec**: `../plans/story-generation-plan.md` §H · **Requirements**: `../requirements/requirements.md` (r2) · **Personas**: `personas.md`

**Reading conventions**
- **Structure** (§H-2): 3 bounded contexts → 6 subdomain epics → stories in **journey order**. Go top-level packages = `Scan` / `Findings` / `Platform`.
- **Checker composition (r3)**: Checking = 2 families — **Observation** (in-process native engine, 10 items: PS-003/008/009/010/011/012/013/018/022/023) + **Probe** (7 external CLI: humble/httpx/testssl/dirhunt/changeme/Corsy/subzy, 12 items). **Delegated** 4 (PS-014→ASM; PS-005/006/026→Nuclei stage). chiller covers 22. **No ZAP, no Nuclei inside chiller.** (r3 supersedes any "ZAP/3-family/25-item" wording below.)

**Architecture principle — control plane vs data plane** *(added 2026-07-08)*
`Platform{Orchestration}` is the **control plane**: it *drives* execution — FSM state transitions (US-024), bounded-concurrency fan-out (US-026), resume via CAS (US-025), partial-failure isolation (US-027), and per-checker progress (US-028). It does **not** hold domain logic.
`Scan{Ingestion, Checking, Safety}` and `Findings{Normalization, Reporting}` are the **data plane**: all "what to check / how to judge / how to normalize / how to account" logic lives here.

Execution flows one way and dependency points one way:
```
Orchestration ──drives──▶  Scan (Ingestion → Safety gate → Checking) ──findings──▶  Findings (Normalization → Reporting)
```
- **Dependency is unidirectional**: Orchestration invokes the data plane; the data plane MUST NOT reference Orchestration. This is exactly the checker plugin seam **NFR-02** (the seam coolant lacks) — a checker/tool can be added or removed without touching the orchestrator.
- **No domain logic in the control plane**: e.g. "robots.txt sensitive path → PS-008" (US-008) belongs to Checking, never to Orchestration; Orchestration only decides *when/how* it runs.
- **Scope note**: the precise component wiring (who injects/calls whom, the dependency graph across the 3 packages) is pinned at **Application Design**. This principle fixes only the direction and the plane separation.

- **Format** (§H-3): classic `As a … I want … so that …` by default; **Job Story** (`When … I want to … so I can …`) only for the 4 state-transition stories (resume, partial failure, rollback, rate-limit trip). Rule: subject = actor intent → classic; subject = system state transition → Job Story.
- **Acceptance criteria** (§H-5): Given/When/Then. `Then` clauses are written so they promote directly to PBT oracles. No intent language.
- **Tags**: `[MVP]` = KISA-only shipping scope · `[Post-MVP]` = deferred (§H-7).
- **`Traces:`** links each story to FR/NFR/SECURITY/PS-ID; the reverse coverage table (§ end) proves every FR is covered.
- **`Property:`** appears where a story anchors a requirements §7 PBT property.

---

# 🟦 Bounded Context: Scan

## Epic E1 — Ingestion

### US-001 — 스캔 요청 제출 `[MVP]`
**As a** 파이프라인 통합자 (P4), **I want** 사전 수집된 target URL 목록과 스코프/옵션(throttle, 도구 선택)으로 스캔을 요청 **so that** 크롤링 단계 없이 곧바로 관찰 기반 점검을 시작할 수 있다.

- **Given** 유효한 URL 목록과 스코프가 담긴 요청, **When** 스캔을 제출하면, **Then** 스캔이 생성되고 스캔 id가 반환된다.
- **Given** chiller는 discovery를 하지 않는다는 전제, **When** 요청이 접수되면, **Then** 목록에 없는 URL은 어떤 처리 대상에도 포함되지 않는다.

**Traces:** FR-01 · **Persona:** P4 · **INVEST:** 독립적(다른 스토리 없이 요청 접수만으로 가치)

### US-002 — 단일 요청 재사용 (cross-family, ResponseCache) `[MVP]` *(r3 강화)*
**As a** 플랫폼/SRE 운영자 (P3), **I want** 각 in-scope URL의 HTML 응답이 스캔당 한 번만 fetch되어 **ResponseCache(Valkey, HAR 엔트리)**에 저장되고 HTML-fetching 체커(관찰 엔진·humble·httpx)가 캐시에서 읽기를 **so that** 도구 계열을 넘어 fetch가 재사용되고 대상 호스트 부하가 줄어든다 (P6 보호).

- **Given** 하나의 in-scope URL, **When** 스캔이 실행되면, **Then** 그 URL에 대한 HTML out-going GET 횟수 = 1 이다 (관찰·humble·httpx 합산; 재개 시에도 — US-025).
- **Given** 캐시된 응답, **When** 관찰 엔진과 URL-scoped 프로브가 판정하면, **Then** 재요청 없이 캐시(HAR)를 읽는다.
- **Given** 스캔 종료, **When** 정리하면, **Then** ResponseCache는 wipe되고(TTL) at-rest 암호화 상태였다 (NFR-16 · SECURITY-01 · FR-12 경계).
- **Given** 프로토콜 특화 도구(testssl/subzy/dirhunt), **When** 실행되면, **Then** 캐시를 우회해 자체 I/O한다 (EC3).

**Traces:** FR-02 · NFR-13 · NFR-16 · SECURITY-01 · **Persona:** P3 (P6 수혜)

### US-003 — 스캔 요청 입력 검증 `[MVP]`
**As a** 파이프라인 통합자 (P4), **I want** 스캔 요청 입력(URL 형식/호스트 allowlist, 목록 크기 상한, 옵션 범위)이 검증되기를 **so that** 잘못된 요청이 조용히 이상 동작하지 않고 즉시 거부된다.

- **Given** 형식이 잘못된 URL 또는 범위를 벗어난 옵션, **When** 요청을 제출하면, **Then** 요청은 거부되고 어떤 아웃바운드 요청도 발생하지 않는다.
- **Given** 목록 크기가 상한을 초과, **When** 요청을 제출하면, **Then** 거부 사유가 응답에 명시된다.

**Traces:** FR-01 · SECURITY-05 · SECURITY-15(fail-closed) · **Persona:** P4

---

## Epic E2 — Safety

### US-004 — 사전 승인 게이트 `[MVP]`
**As a** 대상 시스템 소유자 (P6), **I want** 사전 승인되지 않은 타겟은 어떤 아웃바운드 요청이 나가기도 전에 거부되기를 **so that** 승인하지 않은 내 호스트가 스캔당하지 않는다.

- **Given** 승인 목록에 없는 타겟, **When** 스캔이 그 타겟을 처리하려 하면, **Then** 처리는 거부되고 그 타겟으로의 out-going 요청 수 = 0 이다 (fail-closed).
- **Given** 프로브(testssl.sh, dirhunt, changeme, Corsy, subzy)와 네이티브 GET, **When** 실행되면, **Then** 모두 사전 승인 게이트를 통과한 타겟에 대해서만 동작한다.
- **Given** 스캔 실행 이력, **When** 감사하면, **Then** 누가 언제 무엇을 스캔했는지 append-only로 기록되어 있다.

**Traces:** FR-10 · SECURITY-11 · SECURITY-14 · SECURITY-15 · **Persona:** P6 · **INVEST:** 이 스토리가 P6의 "주인 없는 요구사항" 문제(§C1)를 해소

### US-005 — 레이트 리밋 초과 시 억제 `[MVP]` *(Job Story)*
**When** 한 대상 호스트로 나가는 요청이 설정된 레이트 리밋에 도달하면, **I want to** 초과분을 지연/억제하고 그 사실을 관측 가능하게 남기기를 **so I can** passive 스캔이 대상 서비스를 과부하시키지 않게 한다 (scanner-as-DoS 방지).

- **Given** 대상별 레이트 리밋 R, **When** 아웃바운드 요청률이 R에 도달하면, **Then** 추가 요청은 R을 넘지 않도록 억제되고 초과 이벤트가 메트릭으로 노출된다.
- **Given** 여러 타겟, **When** 동시에 스캔하면, **Then** 레이트 리밋은 타겟별로 독립 적용된다.

**Traces:** NFR-13 · SECURITY-11 · **Persona:** P6 (P3 관측) · **형식 사유(§H-3):** 트리거(리밋 도달)가 본질 → Job Story

---

## Epic E3 — Checking

> **r3 재편 (ZAP·Nuclei 제거)**: Checking은 2계열 — **Observation(in-process native engine, 10)** + **Probe(external CLI, 7도구/12항목)**. ZAP가 담던 헤더/쿠키→humble, 버전→httpx, 본문/well-known 시그니처→Observation 엔진, 에러유발(005/006)·open redirect(026)→Nuclei 스테이지 위임.

### US-006 — 관찰 엔진 프레임워크 (Observation native engine) `[MVP]`
**As a** 보안 분석가 (P2), **I want** in-process 관찰 엔진이 각 URL의 취득 응답에 대해 10개 관찰 시그니처를 돌리기를 **so that** ZAP(JVM 상주) 없이, 능동 공격 없이 관찰만으로 약점을 잡는다. *(개별 항목 판정은 US-008~013·US-030~033)*

- **Given** URL 1회 fetch로 얻은 공유 응답, **When** 관찰 엔진이 판정하면, **Then** 재요청 없이 그 응답에서 등록된 모든 URL-scoped 시그니처가 실행된다 (FR-02).
- **Given** 관찰 엔진이 방출하는 finding, **When** 정규화하면, **Then** 각 finding은 프레임워크가 아니라 CWE로 식별된다 (컴플라이언스 무관).
- **Given** 새 관찰 시그니처 추가, **When** 등록하면, **Then** 오케스트레이터 변경 없이 seam에 편입된다 (NFR-02).

**Traces:** FR-03 · FR-05 · NFR-02 · **Persona:** P2

### US-007 — 프로브 어댑터 (외부 CLI 7도구) `[MVP]`
**As a** 보안 분석가 (P2), **I want** 버전핀된 외부 CLI 7종(humble, httpx, testssl.sh, dirhunt, changeme, Corsy, subzy)을 균일한 어댑터 인터페이스 뒤에 통합 **so that** 이질적 입출력이 하나의 CWE-keyed Finding으로 표준화되고, subprocess 모델로 통일된다.

- **Given** 각 프로브, **When** 어댑터가 실행하면, **Then** 타겟 입력이 표준화되고 결과가 해당 PS-ID(humble→001/015/016/017/024, httpx→007/020, testssl→002, dirhunt→004, changeme→019, Corsy→021, subzy→025)의 CWE로 파싱된다.
- **Given** URL-scoped(humble/httpx/Corsy/changeme) vs host-scoped(testssl/dirhunt/subzy) 구분, **When** 스케줄되면, **Then** 각 도구는 자신의 scope에 맞는 WorkItem으로 실행된다 (C-6).
- **Given** 한 프로브의 비정상 출력, **When** 어댑터가 파싱하면, **Then** panic 없이 처리되고 실패는 US-027(부분 실패)로 전달된다.

**Traces:** FR-04 · **Persona:** P2 · **Note:** PS-025(subzy)는 `no-canonical-cwe` 마커 대상(A-3); nuclei·ZAP 불포함

### US-008 — 네이티브: robots.txt 민감경로 (PS-008) `[MVP]`
**As a** 보안 분석가 (P2), **I want** robots.txt의 민감 경로 노출을 tight anchor로 판정 **so that** 오탐 없이 PS-008만 정확히 잡는다.

- **Given** 민감 경로를 담은 robots.txt 픽스처, **When** 네이티브 체커가 판정하면, **Then** PS-008 finding 1건이 방출된다.
- **Given** 양성(benign) robots.txt 픽스처, **When** 판정하면, **Then** finding 0건이다.

**Traces:** FR-05 · PS-008 · **Persona:** P2

### US-009 — 네이티브: .env 파일 노출 (PS-011) `[MVP]`
**As a** 보안 분석가 (P2), **I want** `.env` 노출을 `DB_PASSWORD=` / `APP_KEY=` 같은 tight anchor로 판정 **so that** 임의의 텍스트 파일을 오탐하지 않는다.

- **Given** `DB_PASSWORD=`를 포함한 `.env` 응답 픽스처, **When** 판정하면, **Then** PS-011 finding 1건이 방출된다.
- **Given** 200이지만 anchor 문자열이 없는 응답, **When** 판정하면, **Then** finding 0건이다.
- **Given** 방출된 evidence, **When** 로그에 남기면, **Then** 비밀 값은 redact된다 (SECURITY-03).

**Traces:** FR-05 · PS-011 · SECURITY-03 · **Persona:** P2

### US-010 — 네이티브: 백업 파일 노출 (PS-012) `[MVP]`
**As a** 보안 분석가 (P2), **I want** `.bak/.zip/.old` 백업 파일 노출을 tight anchor로 판정 **so that** 정상 다운로드 자원을 오탐하지 않는다.

- **Given** 접근 가능한 백업 파일 픽스처, **When** 판정하면, **Then** PS-012 finding 1건이 방출된다.
- **Given** 404/403 응답, **When** 판정하면, **Then** finding 0건이다.

**Traces:** FR-05 · PS-012 · **Persona:** P2

### US-011 — 네이티브: 관리자 페이지 공개 접근 (PS-013) `[MVP]`
**As a** 보안 분석가 (P2), **I want** 관리자 페이지의 **인증 없는 공개 접근**을 tight anchor로 판정 **so that** 인증으로 보호된 관리자 페이지를 오탐하지 않는다 (오탐 고위험 항목).

- **Given** 인증 없이 200으로 노출된 관리자 페이지 픽스처, **When** 판정하면, **Then** PS-013 finding 1건이 방출된다.
- **Given** 관리자 경로가 401/403/로그인 리다이렉트를 반환, **When** 판정하면, **Then** finding 0건이다.

**Traces:** FR-05 · PS-013 · **Persona:** P2 · **INVEST:** 개별 승격 사유 = "부재/접근성 증명"이라 앵커가 고유(§H-4)

### US-012 — 네이티브: crossdomain.xml 과도 허용 (PS-022) `[MVP]`
**As a** 보안 분석가 (P2), **I want** `crossdomain.xml`의 `domain="*"` 과도 허용을 tight anchor로 판정 **so that** 제한적 crossdomain 정책을 오탐하지 않는다.

- **Given** `domain="*"`를 포함한 crossdomain.xml 픽스처, **When** 판정하면, **Then** PS-022 finding 1건이 방출된다.
- **Given** 특정 도메인만 허용한 crossdomain.xml, **When** 판정하면, **Then** finding 0건이다.

**Traces:** FR-05 · PS-022 · **Persona:** P2

### US-013 — 네이티브: 로그인 CAPTCHA 부재 (PS-023) `[MVP]`
**As a** 보안 분석가 (P2), **I want** 로그인 폼의 CAPTCHA **부재**를 tight anchor로 판정 **so that** CAPTCHA가 있는 로그인을 오탐하지 않는다 (부재 증명, 오탐 고위험 항목).

- **Given** CAPTCHA 요소가 없는 로그인 폼 픽스처, **When** 판정하면, **Then** PS-023 finding 1건이 방출된다.
- **Given** 알려진 CAPTCHA 위젯을 포함한 로그인 폼, **When** 판정하면, **Then** finding 0건이다.

**Traces:** FR-05 · PS-023 · **Persona:** P2 · **INVEST:** 개별 승격 사유 = 부재 증명(§H-4)

> **r3 신규 (ZAP → Observation native 이관)**: 아래 4종은 ZAP가 담던 항목을 관찰 엔진 native 시그니처로 이관한 것. BC5=A(개별 스토리 승격).

### US-030 — 네이티브: HTTP 평문 로그인 폼 (PS-003) `[MVP]`
**As a** 보안 분석가 (P2), **I want** `http://`(비-TLS)로 서빙되는 password 입력 폼을 tight anchor로 판정 **so that** HTTPS 로그인 폼을 오탐하지 않는다.

- **Given** `http://`로 서빙되고 `type=password` 필드를 가진 폼 픽스처, **When** 판정하면, **Then** PS-003 finding 1건이 방출된다.
- **Given** `https://`로 서빙된 동일 폼, **When** 판정하면, **Then** finding 0건이다.

**Traces:** FR-03 · FR-05 · PS-003 · **Persona:** P2

### US-031 — 네이티브: 클라이언트 전달 소스 민감정보 (PS-009) `[MVP]` *(r4 확장)*
**As a** 보안 분석가 (P2), **I want** 응답 본문의 **HTML 주석 + 정적 fetch된 JS**(주석·하드코딩 시크릿·내부 엔드포인트)를 tight anchor로 판정 **so that** 클라이언트에 전달된 소스의 민감정보 노출을 잡되 무해한 주석/코드를 오탐하지 않는다 (AC5=A: native 단독; CWE-615/540/200).

- **Given** 민감 문자열(내부 IP·API키·크리덴셜)을 담은 HTML 주석 또는 JS 픽스처, **When** 판정하면, **Then** PS-009 finding 1건이 방출된다.
- **Given** 무해한 주석/코드만 있는 응답, **When** 판정하면, **Then** finding 0건이다.
- **Given** HTML의 `<script src>`(승인 호스트), **When** 발견하면, **Then** 그 JS를 GET(실행 없음)해 캐시에 넣고 판정 대상에 포함한다 (C-8, FR-10).
- **Given** 미승인/서드파티 호스트의 `<script src>`, **When** 발견하면, **Then** fetch하지 않고 로깅한다 (GC2).
- **Given** 방출된 evidence, **When** 로그에 남기면, **Then** 민감 값은 redact된다 (SECURITY-03).

**Traces:** FR-03 · FR-05 · FR-10 · PS-009 · SECURITY-03 · **Persona:** P2 · **Note:** 동적 주입 JS는 Post-MVP 정적 청크 열거(A-6), 헤드리스 없음

### US-032 — 네이티브: .git 디렉터리 노출 (PS-010) `[MVP]`
**As a** 보안 분석가 (P2), **I want** `.git/config`·`.git/HEAD` 노출을 tight anchor로 판정(`.env`(PS-011)과 동일 well-known-path 패턴) **so that** 404/403을 오탐하지 않는다.

- **Given** 접근 가능한 `.git/HEAD`(`ref: refs/` 앵커) 픽스처, **When** 판정하면, **Then** PS-010 finding 1건이 방출된다.
- **Given** `.git/` 경로가 404/403, **When** 판정하면, **Then** finding 0건이다.

**Traces:** FR-03 · FR-05 · PS-010 · **Persona:** P2 · **Note:** nuclei가 아닌 native 관찰 (BC2=A)

### US-033 — 네이티브: URL 내 세션ID 노출 (PS-018) `[MVP]`
**As a** 보안 분석가 (P2), **I want** URL 쿼리/경로에 노출된 세션 식별자(예: `jsessionid=`, `sid=`)를 tight anchor로 판정 **so that** 일반 파라미터를 오탐하지 않는다.

- **Given** `jsessionid=`(또는 알려진 세션 파라미터)를 포함한 URL, **When** 판정하면, **Then** PS-018 finding 1건이 방출된다.
- **Given** 세션 식별자가 없는 URL, **When** 판정하면, **Then** finding 0건이다.

**Traces:** FR-03 · FR-05 · PS-018 · **Persona:** P2

---

# 🟩 Bounded Context: Findings

## Epic E4 — Normalization

### US-014 — 도구 출력 → CWE 기반 Finding 정규화 `[MVP]`
**As a** 보안 분석가 (P2), **I want** 7종 이질적 도구 출력이 **CWE를 정규 정체성으로 하는 단일 Finding 스키마**로 정규화 **so that** 도구가 무엇이든 "무엇이 관찰되었나"를 CWE로 일관되게 다룬다.

- **Given** 알려진 도구 출력 픽스처, **When** 정규화하면, **Then** 알려진 CWE/severity를 가진 Finding이 산출된다 (oracle).
- **Given** 손상된(malformed) 도구 출력, **When** 정규화하면, **Then** panic 없이 처리된다 (parsing is total).
- **Given** 어떤 체커든, **When** Finding을 방출하면, **Then** 그 Finding은 프레임워크(PS-ID)를 포함하지 않는다 — 방출 시점엔 framework-agnostic이다.

**Traces:** FR-06 · NFR-03 · **Persona:** P2 · **Property:** "Tool-output → Finding normalization" (oracle/round-trip, requirements §7)

### US-015 — Finding 중복 제거 `[MVP]`
**As a** 보안 분석가 (P2), **I want** 여러 체커에서 올라온 동일 약점이 중복 제거되기를 **so that** 한 항목이 여러 번 판정되어 리포트를 오염시키지 않는다.

- **Given** 중복을 포함한 finding 집합 x, **When** dedup을 적용하면, **Then** `dedup(dedup(x)) = dedup(x)` (멱등).
- **Given** 여러 체커의 출력, **When** 병합하면, **Then** 결과 집합은 병합 순서에 무관하다 (commutative).

**Traces:** FR-06 · **Persona:** P2 · **Property:** "Finding dedup" (idempotence, requirements §7)

### US-016 — Severity 매핑 `[MVP]`
**As a** 보안 분석가 (P2), **I want** 모든 도구의 severity가 하나의 유효 척도로 전사(total mapping)되기를 **so that** severity 값을 근거 있게 신뢰한다.

- **Given** 임의의 도구 severity, **When** 매핑하면, **Then** 항상 유효한 severity가 산출된다 (총함수).
- **Given** 방출된 모든 finding, **When** 검사하면, **Then** 각 finding은 유효 severity를 가진다.

**Traces:** FR-06 · **Persona:** P2 · **Property:** "Severity mapping" (invariant, requirements §7)

### US-017 — 컴플라이언스 프레임워크 매핑 (CWE → KISA PS-ID) `[MVP]`
**As a** 컴플라이언스 담당자 (P1), **I want** 선언적·버전관리되는 매핑 테이블이 CWE finding을 KISA PS-ID로 투영 **so that** 체커를 건드리지 않고도 CWE를 프레임워크 언어로 읽는다.

- **Given** 체커가 방출할 수 있는 임의의 CWE, **When** 매핑을 조회하면, **Then** ≥1개의 프레임워크 항목 또는 명시적 `no-canonical-cwe` 마커를 반환한다 — 침묵하지 않는다 (totality).
- **Given** N:M 매핑, **When** finding f를 프레임워크 F로 투영하면, **Then** `project(f,F)`의 각 항목은 `f.cwe_ids`의 최소 1개 CWE로 역매핑된다 (아무 항목도 날조되지 않음).
- **Given** 리포트, **When** 생성하면, **Then** 사용된 매핑 테이블 리비전이 기록된다.

**Traces:** FR-14 · NFR-15 · **Persona:** P1 · **Property:** "CWE → framework projection totality" + "N:M round-trip soundness" (requirements §7)

### US-018 — 프레임워크 확장 (OWASP Top 10 · CAPEC) `[Post-MVP]`
**As a** 파이프라인 통합자 (P4), **I want** 새 컴플라이언스 프레임워크를 **매핑 테이블 추가만으로** 지원 **so that** 체커·오케스트레이터·Finding 스키마를 바꾸지 않고 OWASP Top 10·CAPEC 뷰를 제공한다.

- **Given** OWASP Top 10(또는 CAPEC) 매핑 테이블을 추가, **When** 리포트를 생성하면, **Then** 어떤 체커·스키마 코드도 변경되지 않는다.
- **Given** 새 프레임워크 매핑 추가, **When** PS-ID 커버리지 회계를 재계산하면, **Then** 회계 결과는 비트 단위로 동일하다 — 표현 축은 회계 축을 움직일 수 없다.
- **Given** CAPEC 투영, **When** 조회하면, **Then** MITRE `Related_Weaknesses` 관계를 통해 CWE에서 파생된다 (새 정규 축이 아님).

**Traces:** FR-14 · NFR-15 · **Persona:** P4 · **Property:** "Projection does not create coverage" (invariant, requirements §7) — *FR-14가 FR-13을 오염시키지 않음을 보증하는 핵심 property*

---

## Epic E5 — Reporting

### US-019 — 통합 컴플라이언스 리포트 `[MVP]`
**As a** 컴플라이언스 담당자 (P1), **I want** 모든 체커의 finding이 하나의 컴플라이언스 리포트로 병합되기를 **so that** 자산 하나의 상태를 단일 문서로 감사에 제출한다.

- **Given** 여러 체커의 정규화된 finding, **When** 리포트를 생성하면, **Then** CWE 식별 finding과 PS-ID 회계가 **둘 다** 포함되고 어느 쪽도 읽는 시점에 파생되지 않는다.
- **Given** 리포트, **When** 열람하면, **Then** finding(표현 축)과 커버리지(회계 축)가 구분되어 제시된다.

**Traces:** FR-06 · **Persona:** P1

### US-020 — 26개 항목 커버리지 회계 `[MVP]`
**As a** 컴플라이언스 담당자 (P1), **I want** 매 스캔의 리포트가 26개 PS-ID를 각각 `found`/`not-applicable`/`delegated`/`tool-failed` 중 하나로 회계 **so that** 조용한 누락 없이 커버리지 완전성(성공 지표, A-4)을 증명한다.

- **Given** 임의의 스캔, **When** 회계하면, **Then** `found ∪ not-applicable ∪ delegated ∪ tool-failed = 26개 PS-ID 전체` 이고 네 집합은 서로소(pairwise disjoint)다.
- **Given** 어떤 항목도, **When** 회계하면, **Then** 4상태 외의 값을 가지지 않는다.

**Traces:** FR-13 · **Persona:** P1 · **Property:** "Coverage accounting" (invariant, requirements §7) — *성공 지표의 정의 그 자체*

### US-021 — 위임 표기 (target 라벨 포함) `[MVP]` *(r3 일반화)*
**As a** 컴플라이언스 담당자 (P1), **I want** 위임 항목 4종이 `delegated`로, **누구에게 위임됐는지 target 라벨과 함께** 표기되기를 **so that** 커버리지 갭이나 미발견과 혼동하지 않고, 어느 스테이지가 그 항목을 책임지는지 안다.

- **Given** 임의의 스캔 리포트, **When** PS-014를 조회하면, **Then** 상태 = `delegated`, target = `ASM`.
- **Given** 임의의 스캔 리포트, **When** PS-005·006·026을 조회하면, **Then** 상태 = `delegated`, target = `Nuclei`.
- **Given** 임의의 `delegated` 항목, **When** 조회하면, **Then** 상태는 `gap`도 `not-found`도 아니며, 회계에서 covered/gap 어느 쪽으로도 집계되지 않는 별도 범주다.
- **Given** 위임 항목, **When** chiller가 실행되면, **Then** chiller는 그 delegate(ASM/Nuclei)를 **런타임 호출하지 않는다** (파이프라인 수준 위임, §2.2).

**Traces:** FR-11 · FR-13 · PS-014/005/006/026 · **Persona:** P1

### US-022 — 최소 증거 보존 `[MVP]`
**As a** 보안 분석가 (P2), **I want** 판정을 정당화할 만큼의 bounded 증거 스니펫만 저장되고 전체 응답 본문은 저장되지 않기를 **so that** finding을 재현하면서도 민감 데이터를 과잉 보존하지 않는다.

- **Given** 방출된 finding, **When** 저장하면, **Then** 전체 응답 본문은 저장되지 않고 bounded 스니펫만 저장된다.
- **Given** 저장된 finding, **When** at rest 상태를 검사하면, **Then** 암호화되어 있다 (SECURITY-01).
- **Given** 민감 증거, **When** 로그에 기록하면, **Then** redact된다 (SECURITY-03).

**Traces:** FR-12 · SECURITY-01 · SECURITY-03 · **Persona:** P2

### US-023 — 상태·리포트 API `[MVP]`
**As a** 파이프라인 통합자 (P4), **I want** 인증된 API로 스캔 상태·체커별 진행률·병합 리포트를 조회 **so that** 외부 시스템이 안정적 계약으로 chiller를 소비한다.

- **Given** 인증된 요청, **When** 상태/리포트를 조회하면, **Then** 스캔 상태·체커별 진행률·리포트가 반환된다.
- **Given** 미인증 또는 권한 없는 요청, **When** 조회하면, **Then** deny-by-default로 거부되고 토큰은 서버 측에서 검증된다.
- **Given** chiller의 자체 API, **When** 응답하면, **Then** 요구된 보안 헤더를 세팅한다 (dogfooding).

**Traces:** FR-09 · SECURITY-04 · SECURITY-08 · **Persona:** P4

---

# 🟨 Bounded Context: Platform

## Epic E6 — Orchestration

### US-024 — 스캔 작업 수명주기 (FSM) `[MVP]`
**As a** 플랫폼/SRE 운영자 (P3), **I want** 스캔 작업을 FSM으로 생성/시작/중지/상태조회 **so that** 어느 스캔이 어느 상태인지 항상 명확하다.

- **Given** 스캔 작업, **When** 생성/시작/중지하면, **Then** 각 전이는 정의된 FSM 상태 간에서만 일어난다.
- **Given** 임의의 시점, **When** 상태를 조회하면, **Then** 정확히 하나의 well-defined 상태를 반환한다.

**Traces:** FR-07 · **Persona:** P3 · **Note:** coolant의 Task FSM 패턴을 복사(AWS-strip)

### US-025 — 중단된 스캔 재개 `[MVP]` *(Job Story)*
**When** 스캔이 중단되었다가 다시 시작되면, **I want to** compare-and-set으로 이미 완료된 작업을 건너뛰고 남은 것만 재개하기를 **so I can** 중복 요청 없이(→ US-002/P6 보호) 정확히 이어서 완료한다.

- **Given** 일부 URL을 이미 처리한 뒤 중단된 스캔, **When** 재개하면, **Then** 이미 완료된 URL로의 out-going 요청은 추가로 발생하지 않는다.
- **Given** 동시 재개 시도, **When** 작업을 집으면, **Then** compare-and-set으로 정확히 하나의 워커만 각 작업을 소유한다 (leftover resume).

**Traces:** FR-07 · FR-02 · **Persona:** P3 (P6 수혜) · **형식 사유(§H-3):** 상태 전이(중단→재개)가 본질 → Job Story

### US-026 — Fan-out 실행 (bounded concurrency) `[MVP]`
**As a** 플랫폼/SRE 운영자 (P3), **I want** 독립 도구/URL 배치가 bounded 동시성으로 병렬 실행되기를 **so that** 처리량을 확보하면서 chiller와 대상 호스트를 보호한다.

- **Given** 독립 작업들, **When** fan-out하면, **Then** 동시 실행 수는 설정된 상한을 넘지 않는다.
- **Given** 병렬 실행, **When** 배치를 배분하면, **Then** 각 아웃바운드는 명시적 timeout을 가진다 (NFR-12).

**Traces:** FR-08 · NFR-12 · NFR-14 · **Persona:** P3

### US-027 — 부분 실패에도 스캔 지속 `[MVP]` *(Job Story)*
**When** 한 체커/도구가 스캔 도중 실패하면, **I want to** 전체 스캔을 중단하지 않고 실패를 격리한 채 나머지를 완료하고 그 실패를 리포트에 명시하기를 **so I can** 어느 PS-ID가 미판정(`tool-failed`)인지 구분한다 (조용히 누락시키지 않음).

- **Given** 한 도구가 실행 중 실패, **When** 스캔이 계속되면, **Then** 다른 도구의 판정은 영향받지 않고 스캔은 abort되지 않는다 (fault isolation).
- **Given** 실패한 도구가 담당하던 PS-ID, **When** 회계하면, **Then** 그 항목은 `tool-failed`로 분류되고 `found`나 `not-applicable`로 오분류되지 않는다.
- **Given** 부분 실패한 스캔, **When** 리포트를 열면, **Then** 어떤 체커가 실패했는지 명시된다.

**Traces:** FR-08 · FR-13 · NFR-12 · **Persona:** P3 (P1 수혜) · **형식 사유(§H-3):** 트리거(도구 실패)가 본질 → Job Story · **INVEST:** §H-6의 4상태 확정 지점

### US-028 — 관측성 / 체커별 진행률 `[MVP]`
**As a** 플랫폼/SRE 운영자 (P3), **I want** 제어·데이터 경로 모두에 OTel 메트릭/로그/트레이스와 체커별 진행률·헬스체크가 노출되기를 **so that** 엔진을 블랙박스가 아니라 관측 가능한 상태로 운영한다.

- **Given** 진행 중인 스캔, **When** 관측하면, **Then** 체커별 진행률 메트릭이 노출된다.
- **Given** 의존 자원(store/queue + 외부 CLI 도구 가용성), **When** 헬스체크하면, **Then** shallow와 deep 체크가 각각 상태를 보고한다.

**Traces:** NFR-11 · **Persona:** P3 · **Note:** coolant `engine/progress.go` 패턴 참조

### US-029 — 무중단 배포 + 자동 롤백 `[Post-MVP]` *(Job Story)*
**When** 배포 후 헬스 메트릭이 임계치를 벗어나면, **I want to** 블루/그린 컷오버를 자동 롤백하기를 **so I can** 나쁜 배포가 스캔 서비스를 끊지 않게 한다.

- **Given** 블루/그린 배포, **When** 새 버전으로 컷오버하면, **Then** 트래픽 전환은 무중단으로 이뤄진다.
- **Given** 컷오버 후 헬스 메트릭이 임계 이탈, **When** 감지되면, **Then** 이전 버전으로 자동 롤백된다.

**Traces:** NFR-08 · NFR-09 · **Persona:** P3 · **형식 사유(§H-3):** 상태 전이(임계 이탈→롤백)가 본질 → Job Story · **Note:** 블루/그린 vs 카나리아 정합은 NFR Design(A-2)

---

# 페르소나 ↔ 스토리 매핑 (E-5)

| 페르소나 | Primary 스토리 | 수혜(간접) |
|---|---|---|
| **P1 컴플라이언스** | US-017, US-019, US-020, US-021 | US-027(부분 실패 가시성) |
| **P2 보안 분석가** | US-006~016, US-022, US-030~033 | US-023 |
| **P3 SRE 운영자** | US-002, US-024, US-025, US-026, US-027, US-028, US-029 | — |
| **P4 파이프라인 통합자** | US-001, US-003, US-018, US-023 | US-017 |
| **P6 대상 시스템 소유자** | US-004, US-005 | US-002, US-025(중복요청 방지) |

---

# 역추적 — FR → 스토리 역방향 커버리지 (E-6)

**모든 FR이 ≥1 스토리로 커버됨을 증명한다.**

| FR | 스토리 | | NFR/SEC | 스토리 |
|---|---|---|---|---|
| FR-01 | US-001, US-003 | | NFR-03 | US-014, US-019 |
| FR-02 | US-002, US-025 | | NFR-08 | US-029 |
| | | | NFR-16 | US-002 |
| FR-03 | US-006, US-008~013, US-030~033 | | NFR-09 | US-029 |
| FR-04 | US-007 | | NFR-11 | US-028 |
| FR-05 | US-008~013, US-030~033 | | NFR-12 | US-026, US-027 |
| FR-06 | US-014, US-015, US-016, US-019 | | NFR-13 | US-002, US-005 |
| FR-07 | US-024, US-025 | | NFR-14 | US-026 |
| FR-08 | US-026, US-027 | | NFR-15 | US-017, US-018 |
| FR-09 | US-023 | | SECURITY-01 | US-022 |
| FR-10 | US-004 | | SECURITY-03 | US-009, US-022, US-031 |
| FR-11 | US-021 (014→ASM; 005/006/026→Nuclei) | | SECURITY-04 | US-023 |
| FR-12 | US-022 | | SECURITY-05 | US-003 |
| FR-13 | US-020, US-021, US-027 | | SECURITY-08 | US-023 |
| FR-14 | US-017, US-018 | | SECURITY-11 | US-004, US-005 |
| | | | SECURITY-14 | US-004 |
| | | | SECURITY-15 | US-003, US-004 |

**결론:** FR-01 ~ FR-14 **전부** 최소 1개 스토리로 커버됨 (14/14). *(r3)* §H-4에 따라 개별 승격된 Observation native **10종**(US-008~013 + US-030~033)과 프로브 프레임워크(US-007, 외부 CLI 7도구 12항목)가 **22개 covered 항목**을 모두 커버하고, **4개 delegated**(PS-014→ASM, PS-005/006/026→Nuclei)는 US-021로 target 라벨과 함께 표기. 회계 22+4=26 (US-020).

---

# PBT Property 앵커링 (E-7) — requirements §7 → 스토리

| requirements §7 property | 앵커 스토리 | 수용 기준 절 |
|---|---|---|
| Tool-output → Finding normalization (oracle/round-trip) | US-014 | "알려진 CWE/severity 산출" + "parsing is total" |
| Finding dedup (idempotence) | US-015 | `dedup(dedup(x)) = dedup(x)` + commutative |
| Coverage accounting (invariant) | US-020 | `found ∪ not-applicable ∪ delegated ∪ tool-failed = 26`, pairwise disjoint |
| Severity mapping (invariant) | US-016 | 총함수 + 모든 finding 유효 severity |
| CWE → framework projection totality (r2) | US-017 | ≥1 항목 또는 `no-canonical-cwe`, 침묵 금지 |
| Projection does not create coverage (r2) | US-018 | 프레임워크 추가 후 회계 비트 동일 |
| Mapping N:M soundness (r2) | US-017 | `project(f,F)` 각 항목이 `f.cwe_ids`로 역매핑 |

세부 per-unit property 표는 Functional Design(CONSTRUCTION)에서 산출 (PBT-01 FULL).

---

# INVEST 준수 요약 (E-3)

| 기준 | 준수 방식 |
|---|---|
| **Independent** | 도메인 에픽 경계로 스토리를 분리. 의존이 불가피한 경우(US-025→US-002, US-020↔US-027) 매핑표에 수혜 관계로 명시. |
| **Negotiable** | 각 스토리는 결과(what)만 규정, 구현(how)은 Functional Design으로 이연. |
| **Valuable** | 모든 스토리에 페르소나 태그 — 가치 수혜자가 명시됨. 내부 기능도 P2/P3의 관점으로 서술. |
| **Estimable** | §H-4 세분화로 크기 균질화 — 네이티브 6종은 항목별, 관찰 14·프로브 5는 묶음. |
| **Small** | 25개 항목을 25개 스토리로 쪼개지 않고 29개 스토리로 통제. |
| **Testable** | 모든 수용 기준이 Given/When/Then, 의도 표현 금지, 7개는 PBT property로 승격. |

---

# 확장 규칙 준수 요약 (E-8)

| Extension | 스토리 단계 준수 | 상태 |
|---|---|---|
| **security-baseline** | SECURITY-05(US-003) · 08/04(US-023) · 11(US-004,005) · 14(US-004) · 15(US-003,004) · 01/03(US-022,009)이 스토리 수용 기준으로 흡수됨. 06/13(infra/공급망)은 Infra Design·Code Generation으로 이연 → 이 단계 N/A. | ✅ Blocking finding 없음 |
| **resiliency-baseline** | FR-08/FR-13/NFR-12의 부분 실패·fault isolation이 US-027(1급 스토리)로 승격, 재개는 US-025, 롤백은 US-029. RTO/RPO·change mgmt 등 프로세스 항목은 NFR Requirements/Design으로 이연 → 이 단계 N/A. | ✅ Blocking finding 없음 |
| **property-based-testing** | requirements §7의 7개 property가 US-014/015/016/017/018/020의 수용 기준에 앵커링됨(위 E-7 표). 세부 per-unit 표는 Functional Design. | ✅ Blocking finding 없음 |
