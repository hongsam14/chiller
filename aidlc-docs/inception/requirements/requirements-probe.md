# Requirements — Probe CLI 배선 증분

## Intent Analysis
- **User request**: 7개 probe CLI(humble · httpx · testssl.sh · dirhunt · changeme · Corsy · subzy)를 실제 배선하여, 현재 `tool-failed`(무판정)로 보고되는 12개 KISA 항목을 실제 판정하게 만든다.
- **Request type**: Enhancement (brownfield) — 기존 seam(`probe.Adapter`/`runner`)에 실 구현을 채움. 신규 컴포넌트 없음.
- **Scope estimate**: Multiple components — 7개 adapter(`adapters.go`) + 툴별 Parser + `Dockerfile`(런타임 베이스 교체 + 바이너리 번들) + 테스트(유닛+통합) + CI integration 잡 + 회귀 테스트 카운트 갱신.
- **Complexity estimate**: Moderate→Complex — 코드 변경 자체는 국소적이나, 런타임 이미지 아키텍처 변경(Q2=B)과 서브프로세스 수명주기(Q4)가 리스크를 올린다.
- **근거 문서**: `infra-plan.md §3` (TOP PRIORITY), `aidlc-state.md` Backlog.

## 전제 (이미 존재, 재사용)
- `runner`: subprocess exec + timeout + proxy-env 주입 + parse hook — **완성·유닛 테스트됨**. 남은 건 툴별 `argv` + `Parser` + 바이너리뿐.
- `argv`가 non-nil이 되면 `Adapter.Wired()` → true, 항목이 tool-failed → checked로 이동.
- 정직성 불변식(2026-07-13, `probe.ErrNotWired`): 실행 못 하는 adapter는 반드시 실패(→ tool-failed), 절대 빈 clean 반환 금지. **회귀 금지.**
- 소유 CWE→PS-ID 매핑(고정, `adapters.go` + `configs/mappings/kisa.yaml`):
  - humble → CWE-319/614/1004/1275/1021 → **PS-001/015/016/017/024** (URL-scoped)
  - httpx → CWE-693 → **PS-007/020** (URL-scoped)
  - Corsy → CWE-346 → **PS-021** (URL-scoped)
  - changeme → CWE-798 → **PS-019** (URL-scoped)
  - testssl.sh → CWE-1327 → **PS-002** (Host-scoped, **DirectOnly** — TLS 핸드셰이크는 origin 직결)
  - dirhunt → CWE-548 → **PS-004** (Host-scoped)
  - subzy → NoCanonicalCWE → **PS-025** (Host-scoped, ATT&CK T1584.001)

## 결정 (Requirements 답변)
| # | 결정 | 값 |
|---|---|---|
| Q1 | 배선 범위 | **A — 7개 전부** |
| Q2 | 런타임 이미지 | **B — 멀티스테이지 + 툴 전용 베이스** (python/bash/openssl 포함 최소 이미지, non-distroless 감수) |
| Q3 | 버전 고정 | **A — 생태계별 관용**: Go=`go install tool@vX.Y.Z`, Python=`pip install tool==X.Y.Z`(+해시), testssl.sh=git tag/commit 체크아웃 |
| Q4 | exec 실행 증명 | **C — 유닛(exec 경로) + 통합(실툴 골든) 둘 다** + 서브프로세스 수명주기 관리 |
| Q5 | Parser 충실도 | **A — JSON 출력 모드 우선 + 골든 픽스처 회귀** |
| Q6 | 미배선 처리 | **A — tool-failed 정직성 불변** (Q1=A라 전부 배선되나 불변식은 유지) |

---

## Functional Requirements

- **FR-P1 (argv)**: 7개 adapter 각각 `domain.Target`(URL/Host/Scheme)를 받아 해당 툴의 실행 인자 슬라이스를 반환하는 `argv` 함수를 갖는다. URL-scoped는 `Target.URL`, Host-scoped는 `Target.Host`를 대상으로 한다.
- **FR-P2 (Parser)**: 툴 stdout(가능하면 JSON, Q5=A)을 `domain.Finding`으로 변환한다. 각 Finding은 그 툴의 소유 CWE만 부여하고, `SourceTool`을 툴명으로, `TargetURL`을 대상으로, `Severity`는 Valid(R4), `Evidence`는 bounded(FR-12)·정규화한다.
- **FR-P3 (판정 없음=clean)**: 툴이 정상 실행됐으나 취약을 찾지 못하면 빈 findings + nil error(→ StatusChecked). 툴 실행 실패(비정상 종료·타임아웃·파싱 실패)는 error(→ StatusToolFailed). 정직성 불변식 유지.
- **FR-P4 (바이너리 번들)**: 7개 툴 바이너리/스크립트를 버전 고정(Q3=A)하여 런타임 이미지에 포함하고, `argv`가 참조하는 실행 파일이 이미지 `PATH`에 존재한다.
- **FR-P5 (proxy 라우팅)**: financial 모드에서 ProxyEligible 툴 6개는 기존 `ConfigureProxy`가 주입한 `HTTP(S)_PROXY`를 통해 실행(변경 불요, 자동 적용). testssl.sh만 DirectOnly로 origin 직결(PR-12c 불변).
- **FR-P6 (coverage 재파티션)**: 배선 후 `TestE2E_UnwiredProbes_…`의 기대 카운트를 갱신 — tool-failed 12→0(전부 배선 시), checked/n/a가 그만큼 상승, 26 항목 파티션 유지.
- **FR-P7 (JSON 출력 강제)**: 각 툴을 JSON 출력 플래그로 호출하고(httpx `-json`, testssl `--jsonfile -`, subzy JSON, dirhunt/Corsy/changeme/humble 각 JSON 옵션), 파서는 그 스키마를 파싱한다. JSON 미지원/옵션 부재가 확인되면 code-gen 단계에서 대체 파싱 방식을 문서화한다.

## Non-Functional Requirements

- **NFR-P1 (서브프로세스 수명주기 — resiliency-baseline, Q4)**: 타임아웃/컨텍스트 취소 시 **프로세스 그룹 전체**를 종료하여 좀비/고아 프로세스를 남기지 않는다. testssl.sh(openssl 반복 spawn)·Python 툴이 자식 프로세스를 낳으므로 `exec.CommandContext`의 직속 자식 kill만으로는 부족 — `Setpgid` + 그룹 kill(음수 PID) 또는 동등 수단을 적용한다. 각 실행은 per-request 타임아웃 경계(기본 30s, NFR-12)를 갖는다.
- **NFR-P2 (동시성 경계)**: probe 서브프로세스는 워커 풀·rate-budget 경계 안에서만 생성된다(기존 SR-4/워커 격리 불변). 다수 툴 동시 실행이 컨테이너 리소스를 고갈시키지 않도록 기존 orchestrator 경계를 준수한다.
- **NFR-P3 (공급망 무결성 — SECURITY-10/13, Q3=A)**: 모든 바이너리/패키지를 버전 고정하고, 베이스 이미지는 digest 핀. Python 의존은 `requirements.txt`(핀트+가능하면 해시). Dependabot이 이동 시 PR.
- **NFR-P4 (이미지 하드닝 — SECURITY-09 수정, Q2=B)**: distroless 포기에 따른 보완 — ① non-root 사용자 유지, ② digest 핀, ③ 최소 slim 베이스(불필요 패키지 미설치), ④ read-only 루트 FS + tmpfs 작업공간, ⑤ 불필요 capability 드롭. **SECURITY-09를 "distroless nonroot 필수"에서 "최소 공격면 nonroot 하드닝 이미지"로 수정** — 의도적·문서화된 리스크 수용.
- **NFR-P5 (실 exec 검증 — Q4=C)**: `defaultExec`의 os/exec·timeout·proxy-env·프로세스그룹 kill 경로를 유닛 테스트로 커버(경량 실 바이너리)하고, 각 실툴 파서는 `//go:build integration` 통합 테스트에서 핀된 실 바이너리 대상 골든 출력으로 검증. CI integration 잡에서 실행.
- **NFR-P6 (테스트 — property-based-testing enabled)**: 파서는 골든 픽스처 회귀 + 해당되는 경우 속성 테스트(예: 어떤 유효 JSON 입력에도 findings의 CWE는 그 툴 소유 집합의 부분집합이고 Severity는 항상 Valid).

## Extension Compliance (this stage)
| Extension | 적용 | 비고 |
|---|---|---|
| security-baseline | ✅ 적용 | SECURITY-10/13(공급망 핀)=NFR-P3; SECURITY-09(distroless)=**수정**, NFR-P4 완화책으로 리스크 수용(사용자 명시 결정 Q2=B). 블로킹 아님(문서화된 override). SECURITY-12(시크릿 out-of-band)=불변. |
| resiliency-baseline | ✅ 적용 | 서브프로세스 수명주기/좀비 방지=NFR-P1; 동시성 경계=NFR-P2; 타임아웃=NFR-12 재사용. |
| property-based-testing | ✅ 적용 | 파서 속성 테스트=NFR-P6. |

## Out of Scope (이 증분 아님)
- observation well-known-path GET 배선(FR-03/05 gap) — 별개 HIGH 항목.
- 시그니처 사전/transport 정확성(br/zstd, 512KB) — 별개.
- Playwright 동적 발견, OTel/OIDC/ResponseCache 등 infra-plan #3b 이후 항목.

## Key Requirements Summary
7개 probe를 **전부** 배선(Q1=A)하되, Python/bash 실행을 위해 런타임을 **멀티스테이지 최소 툴 이미지**로 교체(Q2=B, SECURITY-09 수정+하드닝 보완). 툴은 **JSON 출력**을 파싱(Q5=A), 바이너리는 **생태계 관용대로 버전 고정**(Q3=A). `defaultExec`는 **유닛+통합 둘 다로 검증**하고(Q4=C), 무엇보다 **서브프로세스 프로세스그룹을 확실히 종료해 좀비를 막는다**(NFR-P1). 정직성 불변식(tool-failed)과 26항목 파티션은 유지.
