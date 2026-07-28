# Functional Design — Observation well-known-path GET provider + dictionary 심화 (r10)

**Unit**: scan · **Increment**: r10 · **Requirements**: `requirements-wellknown.md` (approved)
기술 비의존. observation 신규 입력 provider + fan-out 합성 규칙 + 6개 시그니처 anchor/dictionary 규칙(FN 확장 + FP 축소).

## A. Domain Entities / 관계

- **WellKnownPath (신규, 값)** — approved 호스트에 GET할 상대 경로 1개. 소유 시그니처가 자기 path/body guard로 판정하므로 경로 자체는 소유 CWE를 명시하지 않는다(느슨 결합).
- **WellKnown set (신규)** — 고정 상대 경로 목록(엔진 소유, benign). 호스트별로 절대 Target으로 확장.
- **재사용**: `domain.WorkItem`(URLScoped) · `domain.Target` · `domain.ScanContext`(Target+Response) · 기존 12 observation `Signature` · `fetch.Fetcher`(gate+rate) · `exec` URL-scoped 경로 · coverage 회계(불변).
- **관계**: fanOut(distinct approved host) → WellKnown set 확장 → URLScoped WorkItem[] → 기존 exec 경로 → 소유 Signature.Judge(Target.URL=well-known 경로).

### WellKnown 경로 목록 (고정, 소수 — SEC-W2)
| 경로 | 소유 항목 | 비고 |
|---|---|---|
| `/robots.txt` | PS-008 robots | body Disallow 검사 |
| `/.env` | PS-011 env | body 키=값 검사 |
| `/.git/config` | PS-010 git | body `[core]` |
| `/.git/HEAD` | PS-010 git | body `ref: refs/` |
| `/crossdomain.xml` | PS-022 crossdomain | body allow-access `*` |
| `/backup.zip`,`/www.zip`,`/db.sql`,`/index.php.bak`,`/.env.bak` | PS-012 backup | backup-name heuristic |
| `/admin`,`/administrator`,`/manager/html`,`/wp-admin`,`/phpmyadmin`,`/dashboard` | PS-013 admin | 200+no-login |

총 ~16 경로/호스트(호스트당 1회, per-host rate·approval gate 안). backup/admin 목록은 소수 고정.

## B. Business Rules

### B1. fan-out 합성 (FR-W1/W2, Q1=A/Q3=A/Q6=A)
- **PR-W1**: `haveURL`일 때만, `distinctHosts(set.URLs)`의 각 호스트에 대해 WellKnown set을 절대 Target(`scheme://host + path`)으로 확장해 **URLScoped WorkItem**을 append. host별 1회(distinct dedup).
- **PR-W2**: 합성 항목 ID는 기존 sequential 스킴 유지. 정상 URL 항목 뒤에 append(스케줄러가 per-host rate로 자연 분산).
- **PR-W3 (MaxURLs 제외, Q6=A)**: 합성은 ingest **후** fanOut에서 발생 → `MaxURLs`(ingest의 `len(req.URLs)` 검사)에 카운트되지 않음. 추가 코드 불요(구조상 보장).
- **PR-W4 (안전, SEC-W1/RES-W1)**: 합성 Target은 기존 `Fetcher.Fetch` 경로 → approval gate(SR-3)·per-host token bucket(SR-4)·per-request timeout(SR-8) 그대로. 미승인 호스트면 fetch가 fail-closed(추가 게이트 불요).
- **PR-W5 (정직성, FR-W3)**: well-known GET이 404/비취약이면 소유 시그니처가 nil/N-A → checked-clean(정직). fetch 실패는 해당 합성 항목만 tool-failed(격리, RES-W2) — 동일 checker가 정상 제출 URL에서 ran-ok면 coverage는 "applicable-ok anywhere wins"로 checked 유지.
- **PR-W6 (coverage 불변, FR-W5)**: 합성은 URLScoped → `checkersForScope(URLScoped)`가 동일 observation checker 집합 반환. 소유권·26 파티션·회계 의미 불변.

### B2. 시그니처 anchor/dictionary 심화 (FR-W4, Q4=B/C1=A/C2=A)
각 항목: **FN 확장**(놓침↓) + **FP 축소**(오탐↓). 여전히 **1 CWE 소유**. RE2(ReDoS 불가).

- **PS-011 `.env` (envAnchor v2)**:
  - FN: 키셋을 `^\s*[A-Z0-9_]*(SECRET|PASSWORD|API_?KEY|ACCESS_KEY|TOKEN|PRIVATE_KEY)[A-Z0-9_]*\s*=` 로 일반화 → `STRIPE_SECRET_KEY`/`JWT_SECRET`/`MAIL_PASSWORD` 등 접두/접미 변형 포착.
  - FP 축소: (a) 주석행(`#`) 무시, (b) **값이 실제 비밀 형태**여야 발화 — `=` 뒤 non-empty, 길이 ≥ 8, 플레이스홀더 제외(`your_`,`changeme`,`xxx`,`example`,`placeholder`,`<...>`,`${...}`, 빈값). 프레임워크 `.env.example`(빈/placeholder 값) 오탐 방지.
  - Severity 유지(Critical).
- **PS-012 backup (backupNameAnchor v2)** — 확장자 단독이 아니라 **backup-name heuristic**:
  - 발화 조건(택1): 이중확장자 backup(`\.\w+\.(bak|old|save|orig)$`), 클래식 backup 확장자(`\.(bak|old|swp|orig|save)$` 또는 `~$`), backup-keyword 아카이브(`(backup|bak|dump|db|database|www|site)\.(zip|tar|tar\.gz|tgz|sql|7z|gz)$`).
  - FP 축소: 단순 `report.zip`/`data.sql`(backup 키워드 없는 단일 아카이브)는 **미발화**. (합성 목록은 heuristic 만족.)
  - Severity 유지(High), status 200 요구 유지.
- **PS-010 `.git` (gitPath v2)**:
  - FN: `/.git/HEAD` 추가(합성) → body `ref: refs/` 포착. 마커에 `ref: refs/`(HEAD)·`[core]`(config) 유지.
  - FP 축소: path guard를 `strings.Contains(path,"/.git")` → 정규식 `/\.git(/|$)` 로 조여 `/.gitignore`·`/.github/` 오탐 제거.
  - Severity 유지(High), status 200 유지.
- **PS-013 admin (adminPath v2)**:
  - FN: 목록 확장 `/(admin|administrator|administration|manage|manager/html|wp-admin|phpmyadmin|dashboard|backend)(/|$)`.
  - FP 축소(SPA/login): 200 + no password field 에 더해, **login/SPA 마커 배제** — body에 `(?i)(sign[- ]?in|log[- ]?in|login|authentication required)` 또는 SPA 셸(`<div id="root">`,`<app-root`, 매우 짧은 body) 있으면 미발화(불확실→ 발화 안 함).
  - Severity 유지(High).
- **PS-008 robots (robotsSensitive v2)**:
  - FN: 키워드 확장 `^\s*Disallow:\s*/\S*(admin|backup|config|secret|private|internal|database|sql|\.env|logs|db|api)`.
  - FP: Disallow 라인 한정 유지(광역 body match 아님). Severity 유지(Low).
- **PS-022 crossdomain**: 견고 — 규칙 유지(`<allow-access-from ... domain="*"`). 변경 최소.

### B3. 불변 (SR/미션)
- 능동 크롤링 없음(FR-01) — 고정 well-known 경로 GET만(FR-03). 외부 API·probe·platform seam·domain 인터페이스·제품 Dockerfile 불변.

## C. Business Logic Model (흐름)

```
ingest(req) → URLSet (MaxURLs 검사, 합성 제외)
   ↓
orchestrator.fanOut(set, haveURL, haveHost):
   • URLScoped: interleaveByHost(set.URLs) → WorkItem[]        (기존)
   • HostScoped: distinctHosts → WorkItem[]                     (기존)
   • WellKnown(신규): distinctHosts → WellKnownPaths 확장 → URLScoped WorkItem[]  (PR-W1)
   ↓
exec.Run(URLScoped item):
   Fetch(ctx,scanID,Target)  ← approval gate + per-host rate (PR-W4)
   ScanContext{Target(URL=well-known 경로), Response}
   URL-scoped signatures.Judge  ← 소유 시그니처가 심화 anchor로 판정 (B2)
   ↓
foldOutcomes → coverage (URLScoped checker 귀속, 불변 PR-W6)
```

**텍스트 대안**: ingest가 사용자 URL을 MaxURLs 안에서 검증(합성 제외). fanOut이 기존 URL/host 항목에 더해, distinct approved host별로 고정 well-known 경로를 URL-scoped WorkItem으로 합성. 각 합성 항목은 기존 exec 경로(approval gate+per-host rate 포함 Fetch)를 통과하고, Target.URL이 well-known 경로라 소유 시그니처가 심화된 anchor로 판정. coverage 회계는 URL-scoped observation checker 기준이라 불변.

## D. Compliance (enabled extensions)
| 규칙 | 상태 | 근거 |
|---|---|---|
| security-baseline SEC-W1 (approved만) | ✅ | 기존 fetch.gate.Authorize 재사용, fail-closed |
| security-baseline SEC-W2 (benign·고정 소수 경로) | ✅ | ~16 고정 경로, 능동 열거 없음 |
| resiliency-baseline RES-W1/W2 (rate/timeout 상속·격리) | ✅ | Fetcher 재사용, host별 1회, 실패 격리 |
| property-based-testing PBT-W1 (소유 CWE·FP 코퍼스 미발화·FN 발화) | 설계 반영 | B2 FP 축소 규칙 → 코퍼스 단언(Code Gen) |

## E. Code Gen 넘길 상세
- WellKnown 경로 목록의 배치: 코드 상수(observation 패키지) 권장(config 파일 대안 가능; 소수·안정이라 코드 상수로 충분).
- 합성 지점: `fanout.go`에 well-known 루프 추가(observation의 경로 목록 소비).
- 시그니처 v2 anchor 구현 + FP/FN 코퍼스 유닛 + r9 fixture dormant 주석 제거 + 통합 positive.
