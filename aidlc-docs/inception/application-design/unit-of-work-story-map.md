# Story → Unit Map — chiller

**Stage**: INCEPTION → Units Generation (artifact 3/3) · **Date**: 2026-07-08
**Purpose**: prove **every story maps to exactly one owning unit** (no orphans, no double-ownership).

---

## 1. Assignment (owning unit)

| Story | Title | Unit | Tag |
|---|---|---|---|
| US-001 | 스캔 요청 제출 (ScanAPI) | platform | MVP |
| US-023 | 상태·리포트 API | platform | MVP |
| US-024 | 작업 수명주기 FSM | platform | MVP |
| US-025 | 중단 스캔 재개 (CAS) | platform | MVP |
| US-026 | Fan-out (bounded concurrency) | platform | MVP |
| US-027 | 부분 실패 지속 | platform | MVP |
| US-028 | 관측성/진행률 | platform | MVP |
| US-029 | 무중단 배포+자동 롤백 | platform | Post-MVP |
| US-002 | 단일 요청 재사용 (Fetcher+ResponseCache) | scan | MVP |
| US-003 | 스캔 요청 입력 검증 | scan | MVP |
| US-004 | 사전 승인 게이트 | scan | MVP |
| US-005 | 레이트 리밋 | scan | MVP |
| US-006 | 관찰 엔진 프레임워크 | scan | MVP |
| US-007 | 프로브 어댑터 7종 | scan | MVP |
| US-008 | native robots.txt (PS-008) | scan | MVP |
| US-009 | native .env (PS-011) | scan | MVP |
| US-010 | native 백업파일 (PS-012) | scan | MVP |
| US-011 | native 관리자페이지 (PS-013) | scan | MVP |
| US-012 | native crossdomain.xml (PS-022) | scan | MVP |
| US-013 | native CAPTCHA 부재 (PS-023) | scan | MVP |
| US-030 | native 평문 로그인폼 (PS-003) | scan | MVP |
| US-031 | native 클라이언트소스 민감정보 (PS-009, HTML+JS) | scan | MVP |
| US-032 | native .git (PS-010) | scan | MVP |
| US-033 | native URL 세션ID (PS-018) | scan | MVP |
| US-014 | CWE-keyed 정규화 | findings | MVP |
| US-015 | dedup | findings | MVP |
| US-016 | severity 매핑 | findings | MVP |
| US-017 | 컴플라이언스 매핑 (CWE→KISA) | findings | MVP |
| US-018 | 프레임워크 확장 (OWASP/CAPEC) | findings | Post-MVP |
| US-019 | 통합 리포트 | findings | MVP |
| US-020 | 26항목 커버리지 회계 | findings | MVP |
| US-021 | 위임 표기 (target 라벨) | findings | MVP |
| US-022 | 최소 증거 보존 (FindingsStore) | findings | MVP |

---

## 2. Per-unit rollup

| Unit | Story count | Stories |
|---|---|---|
| **platform** | 8 | US-001, 023, 024, 025, 026, 027, 028, 029 |
| **scan** | 16 | US-002, 003, 004, 005, 006, 007, 008, 009, 010, 011, 012, 013, 030, 031, 032, 033 |
| **findings** | 9 | US-014, 015, 016, 017, 018, 019, 020, 021, 022 |
| **domain** | 0 | (contract only — hosts Finding/Checker/ScanContext used by all) |
| **Total** | **33** | all stories assigned exactly once |

**Count check**: platform 8 + scan 16 + findings 9 = **33**. Distinct story IDs = US-001…US-029 (29) + US-030…US-033 (4) = **33**. Delegated items (PS-005/006/026) have no story of their own — they are accounted by US-021. ✔ No orphan, no double-count.

---

## 3. Boundary validation (D-11 / B-9)

- **No orphan story**: every US-001..029 + US-030..033 has exactly one owning unit (§1).
- **No cross-unit story**: each story's acceptance criteria are satisfiable within its unit's components (checked against `components.md` §4). Stories that *touch* another unit at runtime (e.g. US-027 partial failure marks findings' coverage) still **own** in the unit that drives the behavior (platform), consuming the other via the `domain` contract.
- **Delegated items** (PS-014/005/006/026) are not stories of their own; they are accounted by **US-021** (findings). No unit "implements" a delegated item.
- **PS-ID coverage** (from requirements §8): 22 covered items are realized by scan (Observation 10 + Probe 12) — all under scan stories; the 4 delegated are under findings/US-021. Full 26 accounted.

**Result**: unit boundaries are complete and non-overlapping; ready for the per-unit CONSTRUCTION loop (order: domain → findings → scan → platform).
