# Execution Plan — Observation 오탐 저감 (r16)

**Increment**: r16 · **Risk: Medium** · Single unit = `scan` (+ platform orchestrator fold, domain WorkItem, findings dedup)

## Risk Assessment

- **Medium**. 이유: FR-6(well-known 소유-시그니처-only)가 exec + fold(ranCheckers)·domain WorkItem을
  건드림 — r10 fold 정직성 불변식이 민감. OSS 패턴 증류는 FP/FN 리스크가 있으나 FP/FN 코퍼스로
  방어. 순수 in-process(신규 I/O·인프라·인터페이스 없음)라 배포 리스크는 낮음.

## Phase 결정

| Phase | 결정 | 근거 |
|---|---|---|
| Reverse Engineering | **SKIP** | 참조만(coolant), 기존 |
| User Stories | **SKIP** | 사용자-대면 기능 아님(내부 탐지 품질) |
| Functional Design | **EXECUTE (경량)** | 구조 변경 설계 확정 필요: path→owning-CWE 매핑 + WorkItem 전달(FR-6), dedup/asset 전략(FR-7), 증류 시그니처 데이터-테이블 구조 |
| NFR Req/Design | **SKIP** | NFR은 requirements에 열거(RE2/엔트로피/bounded), 신규 기술 없음 |
| Application Design | **SKIP** | 신규 컴포넌트 없음(observation/exec/dedup 내부) |
| Units Generation | **SKIP** | 단일 unit |
| Infrastructure Design | **SKIP** | 인프라 없음(순수 정규식 + config 데이터) |
| Code Generation | **EXECUTE** | 항상 |
| Build and Test | **EXECUTE** | FP/FN 코퍼스 + 구조 회귀 + ground-truth + PBT + 게이트 |
| Operations | **EXECUTE** | production-readiness에 recall 갭(크롤/스코프) Backlog TOP 갱신 |

## Code Generation 개요 (Part 2에서 상세 플랜)

1. **증류·이식** — Code Gen 중 gitleaks(`gitleaks.toml`)·ZAP(param 목록/값패턴)·Nuclei(api-doc 경로+매처)를
   WebFetch로 받아 필요 데이터 추출 → Go RE2 시그니처/데이터-테이블. RE2 미지원은 등가변환 또는 skip 로그. attribution.
2. **PS-009**: secretAnchor → 엔트로피 게이트 + JS-리터럴 가드 + gitleaks 토큰 룰셋(FR-1~3).
3. **PS-018**: ZAP URL 민감정보 모델(FR-8).
4. **PS-013**: Nuclei api-doc 표면 탐지 분기(FR-9).
4b. **PS-009 내부/인프라 노출(FR-10, 갭 B)**: ZAP Private IP Disclosure 증류(RFC1918+EC2) + 비-prod 호스트 휴리스틱.
5. **FR-6**: well-known path→owning-CWE + WorkItem 전달 + exec/fold(ranCheckers) 갱신.
6. **FR-7**: 자산/증적 중복제거(DiscoveredJS host-dedup 또는 dedup same-CWE+evidence).
7. **검증**: FP/FN 코퍼스(blocking) + 구조 회귀 + ground-truth(human #3/#10/#15) + PBT.

## 브랜치 (Q4 의존성 해소)

- main엔 r10(PR #13)·r15(PR #20)의 `signatures.go`가 미병합 → **`feat/compliance-report`(r10+r15 포함)
  에서 `feat/observation-fp` 분기 권장.** PR은 #20 위에 스택(base=`feat/compliance-report`).
  #13/#20 병합 시 base가 main으로 자연 재조정.
- 대안: #13/#20 병합 대기 후 main에서 분기(블로킹).

## EXECUTE 시퀀스

Functional Design(경량) → Code Generation(P1 플랜 + P2) → Build and Test → Operations.
