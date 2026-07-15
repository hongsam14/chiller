# Probe 파서 실측 (#0d) — 요구사항 확인 질문

**목표**: 6개 wired probe의 **positive-finding 파서를 실제 취약 대상 출력으로 검증·재작성**한다.
지금은 툴이 실행·checked까지만 증명됐고, **finding 추출은 미검증** — 특히 humble 파서는 실 스키마(섹션 기반 `"[0. Info]"`, `"[1. Missing HTTP Security Headers]"`)와 완전히 달라 **현재 finding을 0개** 낸다.

## 배경 — 각 툴이 positive finding을 내려면 필요한 대상

| 툴 | PS | positive finding에 필요한 대상 | 구하기 |
|---|---|---|---|
| **humble** | 001/015/016/017/024 | 보안 헤더 누락 사이트 | **쉬움** — 거의 모든 사이트가 일부 누락(example.com도) |
| **httpx** | 007/020 | 버전 배너 노출 서버 | **쉬움** — 로컬 nginx 버전 노출로 재현 |
| **Corsy** | 021 | permissive CORS(Origin 반사) 엔드포인트 | **중간** — 로컬 조작 서버 |
| **dirhunt** | 004 | 디렉터리 리스팅 노출 | **중간** — 로컬 `autoindex on` nginx |
| **testssl** | 002 | 약한 TLS(구프로토콜/cipher/만료인증서) | **중간** — 공개 `badssl.com` 서브도메인(tls-v1-0/rc4/expired 등) |
| **subzy** | 025 | dangling 서브도메인(미청구 서비스) | **어려움/위험** — 안전한 공개 대상 드묾 |

핵심 결정: **실측 대상을 어떻게 확보할지**와 **이번 범위**다.

---

## Question 1
**실측 대상 전략** — 각 툴의 실 positive 출력을 어디서 캡처할까요?

A) **로컬 취약 픽스처(docker-compose) 우선 + TLS만 공개** — 조작된 nginx로 버전배너/dir리스팅/permissive-CORS/헤더누락을 로컬 재현(재현성·CI 골든 최적), testssl은 공개 `badssl.com`(약한 TLS는 로컬 재현이 번거로움). subzy는 별도 취급(아래 Q3).

B) **전부 공개 테스트 대상** — badssl.com(TLS), 헤더 누락 임의 사이트 등 외부만 사용. 준비는 적으나 외부 의존·불안정·일부 취약(dir리스팅/CORS/subzy)은 마땅한 공개 대상 부족.

C) **전부 로컬 픽스처** — subzy·testssl까지 전부 로컬 모사(자체 CA로 약한 TLS 서버, subzy는 fingerprint에 걸리는 모의 CNAME). 완전 자립·재현성 최상이나 TLS/subzy 모사 구축 비용 큼.

X) Other (please describe after [Answer]: tag below)

[Answer]: X. 이미 만든 외부 취약 사이트가 있는데 이걸 활용하는건? OWASP Juiceshop을 써도 되고

## Question 2
**이번 증분 범위** — 6개 파서를 한 번에? 아니면 우선순위?

A) **깨진 것 우선(humble) + 쉬운 것(httpx) 먼저** — 실 스키마 확인된 humble 파서를 섹션 기반으로 재작성하고 httpx 버전탐지 실측 확정. 나머지(Corsy/dirhunt/testssl/subzy)는 대상 확보되는 대로 후속. **작게 확실히.**

B) **전부 6개** — 모든 파서를 이번에 실측 재작성. 대상 6종 전부 확보 필요(범위·시간 큼).

C) **헤더/버전/TLS 4항목 집중(humble·httpx·testssl)** — 공개/로컬로 대상 확보가 확실한 것들. Corsy/dirhunt/subzy는 대상 확보 난이도 때문에 다음 증분.

X) Other (please describe after [Answer]: tag below)

[Answer]: A

## Question 3
**subzy(PS-025 서브도메인 탈취)** 처리 — dangling 서브도메인은 안전한 실측 대상이 드뭅니다.

A) **이번 제외, 파서는 subzy JSON 스펙 문서로만 맞춤** — subzy의 `--output` JSON 형식을 문서/소스로 확인해 파서를 맞추되, 실 dangling 대상 검증은 이월(정직히 "스펙 기반, 미실측" 명기).

B) **모의 대상 구축** — 통제된 dangling CNAME + subzy fingerprint 매칭 모사 서버로 실측(구축 비용 큼).

X) Other (please describe after [Answer]: tag below)

[Answer]: X. 외부 취약 사이트 + OWASP Juiceshop 활용

## Question 4
**골든 픽스처 갱신 방식** — 실 출력을 회귀 테스트에 어떻게 고정할까요?

A) **실 출력을 그대로 `testdata/*.json` 골든으로 커밋** + 파서 유닛 테스트가 그 골든→기대 finding 검증. 캡처는 로컬/CI에서 1회, 이후 결정론적. (기존 방식 유지·강화)

B) **통합 테스트에서 실 대상에 매번 붙어 검증** — 골든 없이 실시간. 외부 의존·불안정.

X) Other (please describe after [Answer]: tag below)

[Answer]: A
