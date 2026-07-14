# Scope Decision — Passive/static core, active-probe coverage, and the session prerequisite

**Date**: 2026-07-14 · **Status**: Decided · **Branch**: `feat/probe-wiring`
**Drives**: probe-wiring increment (r7), infra-plan §2 (financial session), §4 (JS capture).
**Cross-refs**: `requirements-probe.md`, `execution-plan-probe.md §4b`, `infra-plan.md §2`, `aidlc-state.md`.

## Scope statement (the engine's purpose)
chiller은 **(1) 캡처한 백엔드 API 트래픽 + (2) 페이지 노출 자산**에 대한 **정적/수동 분석** 엔진이다.
- **IN scope**: 취득한 응답·헤더·바디·URL·자산의 수동 분석; 고정 well-known 경로의 benign GET; 그 트래픽을 취득하는 캡처 수단(fetch/ingest, ResponseCache/HAR, PlaywrightDiscoverer net-capture, BrowserSession/AuthProxy/LoginKeeper).
- **OUT of scope (원칙)**: 투기적·조작된·공격성 요청을 새로 **생성**하는 능동 탐침. 익스플로잇/퍼징/페이로드 주입은 chiller에 **존재하지 않음**(코드 전수 확인, 2026-07-14).

## 전수 조사 결과 (2026-07-14) — 능동성은 오직 OSS probe 5개에서만 유입
코드·설정·문서 교차 검증. chiller 자체 코드에는 능동 공격 로직 없음.

| 메커니즘 | 담당 PS | 성격 | 스코프 |
|---|---|---|---|
| Observation 10종 (regex) | PS-003/008/009/010/011/012/013/018/022/023 | 캡처 응답/소스/URL 정적분석 + 고정 well-known GET | ✅ IN (PS-012 백업파일명 추측만 약한 투기성) |
| humble | PS-001/015/016/017/024 | 응답 보안 헤더 수동 판독 | ✅ IN |
| httpx | PS-007/020 | 응답에서 버전 판독 | ✅ IN |
| **testssl.sh** | PS-002 | 능동 TLS 열거 (핸드셰이크 다회) | ⚠️ OUT (능동) |
| **dirhunt** | PS-004 | 능동 디렉터리 브루트포스 | ⚠️ OUT (능동) |
| **changeme** | PS-019 | 능동 기본 크리덴셜 시도 | 🔴 OUT (공격성 최고) |
| **Corsy** | PS-021 | 능동 CORS 조작 요청 | ⚠️ OUT (능동) |
| **subzy** | PS-025 | 능동 서브도메인 탈취 정찰 | ⚠️ OUT (능동) |
| Delegated: PS-005/006/026→Nuclei, PS-014→ASM | — | chiller 미실행, 능동 스테이지로 위임 | ✅ 이미 스코프 밖으로 뺌 |
| 캡처/인프라: fetch·ingest·ResponseCache(HAR)·PlaywrightDiscoverer·BrowserSession/AuthProxy/LoginKeeper·safety·retryablehttp | — | 캡처 수단/가드 | ✅ IN |

**스코프 정합 항목 = 17개** (observation 10 + humble 5 + httpx 2), 전부 캡처 응답의 수동 분석. 캡처 인프라 전부 IN.

## Decision 1 — 능동 probe 5개는 유지한다 (커버리지 결정)
KISA 26항목 의무가 PS-002/004/019/021/025를 요구하고, 이들은 수동으로 환원 불가하거나 비용이 크다. 따라서
**testssl·dirhunt·changeme·Corsy·subzy를 chiller 안에 능동으로 유지**한다.
- 트레이드오프: 미션 선언문의 *"observation, not active attack"* 순수성은 완화된다. 이는 **의도적·문서화된 수용**이다.
- 안전장치(기존): approved-host ∩ allowlist fail-closed, per-host rate budget(SR-4), per-request timeout(SR-8), 워커 격리. 능동 probe도 이 경계 안에서만 실행된다.
- 참고: 더 공격적인 능동(PS-005/006/026)은 이미 Nuclei 스테이지로 위임됨 — 능동을 무조건 배제하는 게 아니라 **경계 안에서 선별 수용**하는 것이 이 프로젝트의 일관된 원칙.

## Decision 2 — 세션 설계는 "능동을 위한 전제"가 아니라 "로그인 뒤 표면 커버리지의 전제"다
"능동을 유지하려면 세션 설계가 필수"라는 인과는 **부정확**하다. 정확히는:
- 능동 probe는 **세션 없이도 비인증(로그인 밖) 표면에서 작동**한다. origin 직결로 충분.
- 세션 설계가 필수인 이유는 **로그인 뒤(인증된) 백엔드 API 표면에 도달**하기 위해서이며, 이는 **수동 검사에도 동일하게** 적용된다.

### 능동 probe 5개 중 실제로 세션을 쓰는 건 2개뿐
| probe | PS | 세션 필요 | 근거 |
|---|---|---|---|
| testssl | PS-002 | ❌ | TLS=전송계층·인증 이전; DirectOnly origin 직결(코드 확정, `transport.go`/`adapters.go`) |
| changeme | PS-019 | ❌ | 기본 크리덴셜 "시도"=인증을 얻으려는 행위, 세션 이전 단계 |
| subzy | PS-025 | ❌ | 서브도메인 정찰=DNS/핑거프린트, 대상 앱 세션과 무관 |
| dirhunt | PS-004 | ⚠️ 부분 | 공개 리스팅은 비인증; 인증 영역 열거만 세션 필요 |
| Corsy | PS-021 | ✅ | 크리덴셜 붙은 인증 API의 CORS라야 의미 |

→ **testssl·changeme·subzy는 세션 유무와 무관하게 동일 작동.** 세션과 얽히는 능동은 dirhunt·Corsy 둘.

### 세션 설계가 진짜로 살리는 것 (엔진 전체 요구)
당신 목표 = "캡처한 백엔드 API 트래픽 분석"인데 금융앱의 그 트래픽 대부분이 로그인 뒤에 있음. 세션 설계는:
- 인증 응답을 보는 **수동 검사**: PS-003/009/013/023 + humble/httpx 인증 헤더
- 로그인 뒤 표면의 **능동 dirhunt/Corsy**
- **PlaywrightDiscoverer가 캡처하는 인증 상태 API 트래픽·자산** ← 스코프의 본체
를 함께 살린다. 즉 세션 설계 = **인증 표면 커버리지 전제(능동·수동 공통)**, 능동 전용 전제가 아니다.

## Implications
- **probe 증분(r7)**: 7개 전부 **직결/비-financial로 배선** → 비인증 표면 즉시 커버. 인증 표면(세션 경유)은 §2에서 별도.
- **§2 세션 설계**: JS 실행 종속 토큰이면 무거운 설계(page.evaluate 토큰 추출 or 인증 page 자체 fetch) 필요 — infra-plan §2 Design decision 참조. 이 세션 위로는 dirhunt/Corsy(능동 2) + 수동 검사들이 함께 흐르며 탭 풀 T를 공유(G3).
- **경계 유지**: 능동 probe는 신뢰 경계(approved∩allowlist, rate budget, timeout) 안에서만. chiller에 익스플로잇/퍼징을 추가하지 않는다(스코프 라인).
