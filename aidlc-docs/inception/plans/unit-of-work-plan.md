# Unit of Work Plan — chiller

**Stage**: INCEPTION → Units Generation (Part 1: Planning) · **Date**: 2026-07-08
**Inputs**: requirements r4 · stories (29 + US-030~033) · `../application-design/*` (§7 units preview)
**Outputs (Part 2)**: `../application-design/{unit-of-work.md, unit-of-work-dependency.md, unit-of-work-story-map.md}`

**Definition**: a unit of work = a logical grouping of stories for development. chiller is a **single deployable** (one image, `--role=manager|worker`, A-1) — so units are **logical modules within one Go binary**, NOT independently deployable microservices. The role split (manager/worker) is orthogonal to the module split.

---

## A. Candidate decomposition (to confirm via §C)

Aligns to the 3 bounded contexts + a shared kernel:

| Unit | Module (Go pkg) | Components | Stories |
|---|---|---|---|
| **U0 — domain** (shared kernel) | `internal/domain` | `Finding`, `ScanContext`, `Checker` iface, `CheckerID`, `Severity`, `PSID`, `Coverage`, `Framework`, `HAREntry` | (contract, no stories) |
| **U1 — platform** (control) | `internal/platform` | ScanAPI, ScanOrchestrator, TaskStore, WorkQueue, ProgressReporter | US-001, 023, 024, 025, 026, 027, 028, 029 |
| **U2 — scan** (data) | `internal/scan` | URLIngestor, ApprovalGate, RateLimiter, Fetcher, ResponseCache, CheckerRegistry, ObservationEngine (10 sigs), ProbeAdapter×7 | US-002, 003, 004, 005, 006, 007, 008~013, 030~033 |
| **U3 — findings** (data) | `internal/findings` | Normalizer, Deduplicator, SeverityMapper, ComplianceMapper, CoverageAccountant, ReportBuilder, FindingsStore | US-014, 015, 016, 017, 018, 019, 020, 021, 022 |

**Inter-unit contracts**: `Checker`, `Finding`, `ScanContext` (all in U0). platform → {scan, findings} (one-way, NFR-02); scan & findings → domain only.

---

## B. Execution checklist

### Part 1 — Planning
- [x] B-1. Candidate decomposition (§A) + story map draft
- [x] B-2. Questions (§C)
- [x] B-3. Collect answers — Q1~Q5 전부 A
- [x] B-4. Ambiguity analysis (Step 7) — 모호/결합/모순 없음
- [x] B-5. Approval — 2026-07-08 "yes"

### Part 2 — Generation (after approval)
- [x] B-6. `unit-of-work.md` — unit definitions, responsibilities, code-organization strategy (greenfield)
- [x] B-7. `unit-of-work-dependency.md` — dependency matrix + build/construction order
- [x] B-8. `unit-of-work-story-map.md` — every story → exactly one unit (completeness proof)
- [x] B-9. Validate boundaries; update aidlc-state.md

---

## C. Decomposition questions (fill every `[Answer]:`)

### Question 1 — Unit granularity
어떤 단위로 쪼갤까요?

A) **4 유닛: domain(공유 커널) + platform + scan + findings (AI 권장)** — 3 컨텍스트 + 공유 도메인 타입 유닛. per-unit CONSTRUCTION 루프가 명확, 계약(Finding/Checker)이 먼저 고정됨.

B) 3 유닛 (domain을 platform 또는 각 유닛에 흡수) — 공유 커널 별도 유닛 없음

C) 더 잘게 — scan을 Ingestion/Safety/Checking으로, findings를 Normalization/Reporting으로 (6~7 유닛) — 세밀하나 per-unit 루프 오버헤드↑

D) 단일 유닛 (모놀리식, 내부 모듈만) — 분해 없이 한 번에 설계·구현

X) 기타

[Answer]: A

### Question 2 — 배포 모델 확인 (A-1)
A) **단일 배포체(이미지 1개, manager/worker 역할), 유닛=논리 모듈 (AI 권장)** — A-1 확정 사항. 마이크로서비스 아님.

B) 유닛별 독립 배포(마이크로서비스) — A-1(단일 이미지)과 충돌, 재논의 필요

X) 기타

[Answer]: A

### Question 3 — 구성(Construction) 순서
per-unit 루프(Functional Design→Code Gen)를 어떤 순서로?

A) **domain → findings → scan → platform (의존성 순, AI 권장)** — 계약(domain) 먼저; findings가 Finding/매핑/회계 정의; scan이 그 Finding을 방출; platform이 전체 오케스트레이션. 하위→상위.

B) domain → scan → findings → platform — 수집·체커 먼저

C) platform 먼저 (골격/FSM) → scan → findings — 오케스트레이션 스켈레톤 우선

X) 기타

[Answer]: A

### Question 4 — Go 디렉터리 구조 (greenfield)
A) **표준 Go 레이아웃 (AI 권장)**:
```
/cmd/chiller/           # main, --role flag
/internal/domain/       # U0 공유 커널
/internal/platform/     # U1
/internal/scan/         # U2
/internal/findings/     # U3
/configs/mappings/      # CWE→framework 선언적 파일 (FR-14)
/deploy/                # Helm/OCI (AWS-free)
/test/                  # PBT/통합 픽스처
```

B) 다른 레이아웃 ([Answer]에 서술)

C) 나중에 Code Generation에서 결정

X) 기타

[Answer]: A

### Question 5 — 공유 도메인 타입 위치 (U0)
A) **단일 `internal/domain` 공유 커널 (AI 권장)** — Finding/ScanContext/Checker 등 인터-유닛 계약을 한 곳에. 순환 의존 없음(domain은 아무것도 import 안 함).

B) 각 유닛이 자기 타입 정의 + 어댑터로 변환 — 결합도↓이나 중복·변환 비용

X) 기타

[Answer]: A

---

## D. 확장 규칙 준수 (Planning)
| Extension | 상태 |
|---|---|
| security-baseline | ApprovalGate/RateLimiter를 scan의 Safety 모듈로 격리(SECURITY-11); FindingsStore 암호화는 findings. Blocking 없음 |
| resiliency-baseline | 부분실패/resume은 platform 유닛(US-025/027). Blocking 없음 |
| property-based-testing | §7 property가 findings 유닛(normalize/dedup/severity/mapping/accounting)에 집중 → 그 유닛 Functional Design에서 property 표. Blocking 없음 |
