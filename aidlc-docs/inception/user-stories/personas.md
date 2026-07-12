# Personas — chiller (passive-scanner engine)

**Stage**: INCEPTION → User Stories (Part 2) · **Date**: 2026-07-08
**Basis**: `../plans/story-generation-plan.md` §H-1 (Q1=B) · `../requirements/requirements.md` (r2)
**Scope**: 5 personas. The upstream crawler / downstream consumer is a **system actor** (was candidate P5) and is deliberately *not* a persona — it appears only as a constraint in story bodies (§H-1).

Each persona is a decision-making archetype, not a job title. Stories tag the persona whose goal they serve.

---

## P1 — 컴플라이언스 담당자 (Compliance Owner)

| | |
|---|---|
| **One-liner** | KISA 26개 항목이 매 스캔에서 빠짐없이 회계되었는지를 책임지는 사람. |
| **Type** | 인간 · 리포트 소비자 · 의사결정자 |
| **Primary goal** | "이 자산에 대해 26개 항목이 각각 어떤 상태(found / not-applicable / delegated / tool-failed)인지"를 **한눈에, 빠짐없이** 확인하고 감사에 제출한다. |
| **Success = when** | 어떤 스캔 리포트를 열어도 26개 PS-ID가 하나도 누락 없이 4상태 중 하나로 분류되어 있고, PS-014가 `gap`이 아니라 `delegated`로 명확히 구분된다. |
| **Frustrations (좌절 지점)** | ① 리포트가 "이상 없음"인지 "점검 안 됨"인지 구분되지 않는 것 (조용한 누락). ② PS-014가 커버리지 갭처럼 보여 매번 해명해야 하는 것. ③ 도구 하나가 죽었는데 리포트가 그것을 숨기는 것. |
| **Mental model** | 프레임워크(KISA) 언어로 사고한다. CWE는 몰라도 되지만, 리포트가 CWE→PS-ID로 정확히 투영되었음을 신뢰해야 한다. |
| **Cares about** | FR-11(PS-014 delegation) · FR-13(coverage accounting) · FR-14(framework projection) · A-4(coverage completeness = 성공 지표) |
| **Does NOT care about** | 어떤 도구가 판정했는지, 동시성 파라미터, 배포 방식. |

---

## P2 — 보안 분석가 (Security Analyst)

| | |
|---|---|
| **One-liner** | 방출된 finding을 트리아지하고 오탐을 걸러내는 사람. |
| **Type** | 인간 · finding 소비자 · 검증자 |
| **Primary goal** | 각 finding이 **진짜인지 즉시 판단**할 수 있을 만큼의 증거(evidence)를 가지고, 낭비되는 오탐 없이 실제 약점만 다룬다. |
| **Success = when** | 네이티브 시그니처(특히 PS-013 관리자 페이지, PS-023 CAPTCHA 부재)의 오탐이 억제되어 있고, 각 finding에 판정을 재현할 수 있는 최소 증거 스니펫과 CWE가 붙어 있다. |
| **Frustrations** | ① 앵커가 느슨해 쏟아지는 오탐. ② 증거가 없어 finding을 직접 재현해봐야 하는 것. ③ severity가 근거 없이 매겨진 것. ④ 같은 약점이 여러 체커에서 중복으로 올라오는 것. |
| **Mental model** | CWE 언어로 사고한다. "무엇이(weakness) 관찰되었나"가 관심사이지 "어떤 공격이 가능한가"가 아니다 — 그건 Active-Scanner의 몫. |
| **Cares about** | FR-05(tight anchors) · FR-06(dedup) · FR-12(evidence 보존) · NFR-03(CWE-keyed finding) · SECURITY-03(로그 redaction) |
| **Does NOT care about** | 배포 토폴로지, DR 전략. |

---

## P3 — 플랫폼/SRE 운영자 (Platform / SRE Operator)

| | |
|---|---|
| **One-liner** | chiller 엔진 자체를 돌리고, 부분 실패를 관측하고, 재개·롤백을 책임지는 사람. |
| **Type** | 인간 · 운영자 · 사건 대응자 |
| **Primary goal** | 스캔이 진행 중이든, 부분 실패했든, 중단 후 재개되든 **엔진의 상태를 항상 정확히 관측**하고, 한 도구의 실패가 전체 스캔을 무너뜨리지 않게 한다. |
| **Success = when** | 어느 체커가 실패했고 어떤 PS-ID가 `tool-failed`인지 즉시 보이고, 중단된 스캔이 이미 끝낸 작업을 중복 없이 재개하며, 배포/롤백이 무중단으로 이뤄진다. |
| **Frustrations** | ① 한 도구가 죽어 스캔 전체가 abort되는 것. ② 재개 시 이미 한 요청을 다시 보내 대상 호스트를 두 배로 때리는 것. ③ 진행률이 보이지 않는 블랙박스. ④ 롤백이 트래픽을 끊는 것. |
| **Mental model** | 상태 기계(FSM)와 큐로 사고한다. "지금 어느 상태이고, 실패하면 어디로 가는가". |
| **Cares about** | FR-07(FSM/CAS-resume) · FR-08(fault isolation) · NFR-06~12(DR·롤백·관측성·fault isolation) · NFR-13(throttling) |
| **Does NOT care about** | KISA 항목의 의미론, 어떤 CWE인지. |

---

## P4 — 파이프라인 통합자 (Pipeline Integrator)

| | |
|---|---|
| **One-liner** | chiller의 API에 대고 스캔을 걸고 리포트를 받아가는 외부 시스템을 만드는 사람. |
| **Type** | 인간 · API 소비자 개발자 · (외부 **비통제** 소비자 소유) |
| **Primary goal** | chiller의 스캔 요청 / 상태 / 리포트 **API 계약이 안정적**이어서, chiller가 내부적으로 바뀌어도 자신의 통합 코드가 깨지지 않는다. |
| **Success = when** | Finding 스키마와 리포트 계약이 버전으로 관리되고, 새 컴플라이언스 프레임워크(OWASP·CAPEC)가 추가돼도 기존 응답 계약이 보존된다. |
| **Frustrations** | ① 예고 없는 스키마 변경으로 파서가 깨지는 것. ② PS-ID 회계와 CWE 표현이 뒤섞여 어느 것을 신뢰할지 모호한 것. ③ 인증/인가가 불명확한 것. |
| **Mental model** | 계약(contract)으로 사고한다. 내부 구현이 아니라 스키마·상태 전이·버전이 관심사. |
| **Cares about** | FR-09(status/report API) · NFR-03(단일 Finding 계약) · NFR-15(framework 확장이 계약을 깨지 않음) · SECURITY-08(API authz) · Q3=A(외부 비통제 소비자) |
| **Does NOT care about** | 체커 내부 구현, 인프라. |

---

## P6 — 대상 시스템 소유자 (Target-System Owner)

| | |
|---|---|
| **One-liner** | chiller가 재요청을 보내는 그 호스트를 운영하는 사람 — chiller의 *사용자가 아니라* 영향을 받는 이해관계자. |
| **Type** | 인간 · **비사용자 이해관계자** · 잠재적 피해자 (scanner-as-DoS) |
| **Primary goal** | 자신의 호스트가 **사전 승인된 범위 안에서만, 과부하 없이** 스캔당한다. 승인하지 않은 스캔은 아예 요청조차 도달하지 않는다. |
| **Success = when** | 사전 승인되지 않은 타겟은 어떤 아웃바운드 요청도 발생하기 전에 거부되고(fail-closed), 승인된 타겟도 레이트 리밋 안에서만 요청받는다. |
| **Frustrations** | ① 승인 안 한 자산이 스캔당하는 것. ② passive 스캔이 재요청을 몰아쳐 서비스가 느려지는 것. ③ 누가 언제 무엇을 스캔했는지 추적되지 않는 것. |
| **Mental model** | 방어자로 사고한다. "이 트래픽이 승인된 것인가, 감당 가능한가". chiller의 UI를 직접 보지 않지만, chiller의 안전 정책이 곧 자신의 보호막. |
| **Cares about** | FR-10(pre-approval gate) · NFR-13(throttling) · SECURITY-11(secure design / abuse case) · SECURITY-14(audit of who scanned what) · SECURITY-15(fail-closed) |
| **Does NOT care about** | 리포트 내용, KISA 항목 — 그건 P1의 몫. |

---

## Persona relationships (한눈에)

```
             (URL 목록 투입)                         (리포트 수령)
 상위 크롤러 ───────────────▶  [ chiller ]  ───────────────▶ 하위 소비자
  (시스템 액터)                    │                          (시스템 액터)
                                   │ 재요청 (사전승인·스로틀 하에서만)
                                   ▼
                            P6 대상 시스템 소유자  ◀── 보호 대상 (비사용자)

 운영: P3 SRE ── 엔진 상태·부분실패·재개·롤백
 소비: P4 통합자 ── API 계약   /   P2 분석가 ── finding 트리아지   /   P1 컴플라이언스 ── 26항목 회계
```

- **P1 ↔ P2**: 같은 리포트를 보되 다른 축을 본다 — P1은 PS-ID 회계(무엇이 점검됐나), P2는 CWE finding(무엇이 발견됐나).
- **P3 ↔ P6**: P3의 재개(resume) 정확성과 스로틀링이 곧 P6의 보호다 (중복 요청 = P6 과부하).
- **P4 ↔ (P1·P2)**: P4가 지키는 API 계약 위에서 P1·P2가 리포트를 소비한다.
