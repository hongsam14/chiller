# Code Generation Summary — Probe 파서 실측 (#0d)

**Date**: 2026-07-15 · **Plan**: `construction/plans/parser-fidelity-code-generation-plan.md`
**Target**: OWASP Juice Shop (로컬 docker) + 버전노출 nginx. **Golden**: 실 캡처(Q4=A).
**Verified**: build/vet(+integration)/gofmt/arch-check/`go test ./... -race`/gosec(0) green + 이미지 통합 positive green.

## 무엇을 했나
실 대상 출력으로 파서를 검증·재작성. **humble/httpx/corsy가 실제로 finding을 산출**함을 증명(humble은 이전 0개 → 수정). 그 과정에서 **humble의 쿠키 소유권 오류를 바로잡아 observation으로 이전**.

## 파서 교정 (실 캡처 근거)
| 툴 | 이전(추측) | 실측 교정 |
|---|---|---|
| **humble** | `{missing,cookies}` 스키마 + 쿠키 5CWE | **섹션기반**(`[2. Missing HTTP Security Headers]` 등) 파싱. **헤더만** → PS-001(HSTS 누락→CWE-319)·PS-024(X-Frame **와** CSP 둘 다 부재→CWE-1021). Juice Shop=HSTS 누락·X-Frame set → CWE-319 하나(오탐 없음) |
| **Corsy** | `{results:[…]}` | 실 스키마=맵 `{url:{class,"acao header",severity}}`. ACAO 항목 존재→CWE-346. Juice Shop wildcard ACAO positive |
| **httpx** | (스키마 정확) | 골든을 실 출력(`webserver:nginx/1.27.5`, `tech:["Nginx:1.27.5"]`)으로 갱신. 로직 유지 |

## 쿠키 소유권 이전 (옵션 A)
humble은 **응답 헤더만** 분석하고 Set-Cookie를 안 봄 → PS-015/016/017을 humble이 소유하면 humble clean 시 **false-clean**. 바로잡음:
- **observation에 Set-Cookie 시그니처 3개 추가** — `HAREntry.Cookies`(fetcher가 이미 파싱)에서 Secure/HttpOnly/SameSite 부재 판정(CWE-614/1004/1275). 쿠키 없으면 N/A, SameSite=None도 취약 처리.
- `adapters.go` humble CWE = **319/1021만**. `configs/mappings/kisa.yaml` 쿠키 CWE를 observation 섹션으로. `engine.go` Default() 13종.
- **소유권 이동 결과**: probe-owned 12→9, observation 10→13. boot 26 파티션 유지.

## 커버리지 카운트 이동 (정직성)
- **일반 `go test`(바이너리 부재)**: tool-failed **12→9**(쿠키 3개가 observation으로), delegated 4, observation(checked+na) **10→13**. (`TestE2E_ProbesWithoutBinaries…` 갱신)
- **이미지 통합(실 툴, 취약 대상)**: humble·httpx·corsy가 **실제 finding 산출**(CWE-319/693/346) — `TestIntegration_URLProbes…`가 positive 단언으로 강화.
- **쿠키**: observation 시그니처가 in-process로 즉시 판정(유닛 테스트로 positive/N/A/secured 검증).

## 테스트
- `parsers_test.go`: humble 실 골든→CWE-319 단언 + clickjacking(둘 다 부재) 케이스. corsy 실 골든→CWE-346. httpx 실 nginx 골든.
- `cookie_test.go`(observation): insecure→finding, secured→clean, no-cookie→N/A, SameSite=None→flagged.
- 카운트 테스트 갱신: observation 13, registry ownership 20, e2e 9/4/13.

## ⚠️ 정직한 이월 (다음 증분)
- **testssl(PS-002)·dirhunt(PS-004)·subzy(PS-025)**: Juice Shop 로컬(http)에 약한 TLS·dir 리스팅·dangling 서브도메인이 **없어 positive 미실측**. 파서는 스펙/실행 기반이며 **positive-finding 스키마는 여전히 미검증**. 대상 확보 시 다음 증분(production-readiness #0d 잔여).
- humble PS-024(clickjacking)도 Juice Shop이 X-Frame set이라 positive는 유닛 합성 케이스로만 검증(실 취약 대상 이월).

## 인터페이스 불변 (PR-11)
`Adapter`/`Checker`/`Signature` 시그니처 불변. observation 시그니처 3개 추가 + 소유권 이전만.
