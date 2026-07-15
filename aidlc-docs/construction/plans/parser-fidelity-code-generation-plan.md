# Code Generation Plan — Probe 파서 실측 (#0d)

**Target**: OWASP Juice Shop (로컬 docker) + 버전노출 nginx(httpx positive). **Golden**: 실 캡처(Q4=A).
**Single source of truth.** 코드 위치: `internal/scan/checking/probe/{parsers,adapters}.go` + `testdata/` + 테스트. (aidlc-docs 아님)

## 실측 발견 (실 캡처로 확정)
| 툴 | 발견 | 조치 |
|---|---|---|
| **humble** | 실 스키마=섹션기반(`[2. Missing HTTP Security Headers]` 배열, `[4. Deprecated…]` 배열). **쿠키 미분석**(Set-Cookie 안 봄). | 파서 재작성 → **PS-001(HSTS 누락→CWE-319)·PS-024(X-Frame/CSP 누락→CWE-1021)만** 산출 |
| **httpx** | 스키마 정확(`webserver`/`tech`). 단 **명시 포트 필요**(`http://h:80/`). 실 출력 `webserver:"nginx/1.27.5"`, `tech:["Nginx:1.27.5"]`. | 골든을 실 출력으로 갱신. argv에 포트 보정 검토 |
| **Corsy** | 실 스키마=맵 `{url:{class,"acao header",severity,…}}` (내 `{results:[…]}` 틀림). Juice Shop=wildcard ACAO positive. | 파서 재작성 → `acao header`가 `*`/`null`이면 CWE-346 |

## ✅ 결정 — humble 쿠키 소유권 (PS-015/016/017) = 옵션 A (observation 이전)
humble은 쿠키 플래그를 탐지 못 하고 그냥 뗄 수 없음(boot 26 소유자 필요). **옵션 A 확정**: observation에 Set-Cookie 시그니처 3개 추가(이미 fetch된 응답 파싱, in-process), CWE-614/1004/1275 소유를 `probe:humble` → `observation`으로 이전(`kisa.yaml` + adapters.go). false-clean 제거, PS-015/016/017 실판정.

## Phase (옵션 확정 후 실행)
- [x] **P1 humble 파서 재작성** — 섹션기반 파싱. `[2. Missing]`에서 `Strict-Transport-Security`→CWE-319(PS-001), `X-Frame-Options` 부재 & CSP `frame-ancestors` 부재→CWE-1021(PS-024). `[4. Deprecated]`는 참고. 실 Juice Shop 골든.
- [x] **P2 Corsy 파서 재작성** — 맵 스키마. `acao header ∈ {"*","null"}`(+`acac header`) → CWE-346. 실 Juice Shop 골든.
- [x] **P3 httpx 골든 갱신** — 실 nginx 출력(`webserver:nginx/1.27.5`). 파서 로직 유지(schema 정확). argv 포트 보정 검토(명시 포트 없을 때 httpx 미출력 완화).
- [x] **P4 observation 쿠키 시그니처 3 (옵션 A 확정)** — Set-Cookie에서 Secure/HttpOnly/SameSite 부재 판정(CWE-614/1004/1275). `adapters.go`에서 humble의 쿠키 CWE 제거(humble=319/1021만) + `kisa.yaml` 소유 이전(observation). boot 26 파티션 유지, coverage_wiring 테스트 갱신.
- [x] **P5 골든 커밋 + 유닛 positive** — `testdata/{humble,corsy,httpx}.json` 실 출력으로 교체; 파서 유닛이 **finding ≥1** 단언(humble 현 0 → 수정 증명).
- [x] **P6 통합 positive** — `integration_test.go` 강화: Juice Shop에 붙어 humble·corsy가 **실제 finding** 산출(이미지 내부). httpx는 버전노출 대상.
- [x] **P7 요약** — `construction/scan/code/parser-fidelity-summary.md`: 발견·조치·정직한 이월(testssl/dirhunt/subzy 미실측, 쿠키 처리 결과).

## 수용 기준
- humble/Corsy 파서가 **실 골든에서 finding ≥1** (humble 0→수정). httpx 실 골든 유지.
- 정직성 불변·26 파티션·인터페이스 불변. 로컬 build/vet/gofmt/arch-check/race/gosec green. 이미지 통합 positive green.

## 리스크
- 옵션 A는 매핑/관찰 변경 → boot completeness·coverage 테스트 갱신 필요(회귀 방지).
- httpx 명시포트 이슈: 실 스캔 대상(https 443)에서도 재현되는지 확인(별도).
