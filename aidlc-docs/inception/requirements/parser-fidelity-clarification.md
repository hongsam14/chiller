# Probe 파서 실측 (#0d) — 대상 확정 (짧은 확인)

Q1/Q3에서 "외부 취약 사이트 + OWASP Juice Shop 활용"으로 답하셨습니다. 실 출력을 캡처하려면 **구체적 주소**가 필요합니다.

## Clarification 1 — OWASP Juice Shop 실행 방식
A) **내가 로컬에서 docker로 띄운다** (`docker run -d -p 3000:3000 bkimminich/juice-shop`) — 재현성 최상, 버전 고정 가능, CI 골든 캡처에 이상적. chiller가 `http://localhost:3000/` 대상으로 붙음.

B) **이미 떠 있는 인스턴스에 붙는다** — 주소를 아래에 적어주세요.

X) Other

[Answer]: A

(B라면 Juice Shop URL):

## Clarification 2 — "이미 만든 외부 취약 사이트"
그 사이트를 이번 실측에 **포함**할까요? 포함하면 주소와, 무엇이 취약한지(예: 약한 TLS·dir 리스팅·CORS·버전 노출) 알려주세요 — 특히 **HTTPS(약한 TLS)**가 있으면 testssl까지 실측할 수 있습니다.

A) **포함** — 아래에 URL + 취약 특성 기입.

B) **이번엔 Juice Shop만** — 외부 사이트는 다음 증분(testssl/dirhunt/subzy 대상 확보 시).

X) Other

[Answer]: B

(A라면 URL + 취약 특성):

## 참고 (범위 재확인)
- 이번 증분 = **humble + httpx 우선**(Q2=A). Juice Shop은 헤더 누락(humble)·tech/버전(httpx)·permissive CORS(Corsy까지 가능)를 실제로 가집니다.
- Juice Shop 로컬(http)은 **약한 TLS·dir 리스팅·dangling 서브도메인이 없음** → testssl/dirhunt/subzy는 대상이 생기기 전까지 이월(정직히 "미실측"). 외부 사이트가 HTTPS면 testssl은 이번에 가능.
