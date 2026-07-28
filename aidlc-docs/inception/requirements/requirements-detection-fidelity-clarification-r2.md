# Requirements Clarification R2 — 통합 fixture 커버리지 확정 (#0d 나머지, r9)

R1 확정: **C2=A(재사용 fixture 레포 커밋) · C3=A(스코프 라인 유지 — fixture는 스캔당하는 테스트 대상일 뿐).**

C1은 **B(부분 통합)** 를 고르셨지만 메모로 *"이전 단계에서 probe한 취약점들도 모두 통합하는 건 어떤지?"* 를 주셨습니다. 즉 이번 3개(testssl/dirhunt/subzy)뿐 아니라 r8에서 검증한 **humble/httpx/Corsy** 까지 하나의 로컬 fixture로 묶어 외부 대상(Juice Shop·kblife·badssl) 의존을 없애자는 취지로 읽힙니다. 이러면 fixture가 **엔진 전체 positive-path 회귀 대상**이 됩니다. 다만 C1=B는 testssl을 외부 badssl로 남기므로, "모두 통합"과 정합을 맞추기 위해 커버리지 범위를 확정합니다.

각 질문의 `[Answer]:` 태그 뒤에 알파벳으로 답해 주세요.

---

## Clarification 4: 통합 fixture 커버리지 범위
`test/fixtures/vuln-target/` (docker-compose)가 어디까지 포함할까요?

A) **완전 통합·완전 hermetic (권장) — 6개 probe 전부 로컬** — 하나의 compose에:
   - weak-TLS 엔드포인트(구 프로토콜/약한 cipher openssl) → **testssl** (badssl 대체, 외부 의존 0)
   - autoindex 디렉토리 → **dirhunt**
   - takeover 지문 mock → **subzy**
   - 보안 헤더 누락(HSTS/X-Frame/CSP 없음) → **humble**
   - 버전 배너 노출(예: nginx/1.x) → **httpx**
   - permissive CORS(ACAO 반사) → **Corsy**
   외부 대상 전부 제거, r8 probe까지 hermetic 재검증. **C1=B의 testssl→badssl을 로컬로 override.**

B) **HTTP-only 통합, testssl만 외부 badssl 유지** — humble/httpx/Corsy/dirhunt/subzy 5개를 로컬 fixture로 묶되(모두 HTTP 계층), weak-TLS는 로컬 구성 생략하고 testssl은 badssl로 실측. 외부 의존 1개(badssl) 잔존. C1=B에 충실.

C) **이번 증분 3개만 fixture** — dirhunt+subzy만 로컬(+testssl은 badssl 외부), humble/httpx/Corsy 재검증은 하지 않음(메모 미반영).

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: B

---

## Clarification 5: observation 시그니처 표면 포함 여부
관찰(observation) 계열 항목(PS-008 robots.txt · PS-010 .git · PS-011 .env · PS-012 backup · PS-022 crossdomain.xml · PS-018 session-in-URL · PS-009 comments · 쿠키 등)의 취약 표면도 fixture에 심을까요?

> 참고: 이들 well-known-path는 현재 엔진이 **아직 요청하지 않습니다**(Backlog "🟠 HIGH — Observation well-known-path GETs 미배선"). 표면을 심어도 well-known provider 배선 전까지는 finding이 나지 않습니다. 이건 별개 backlog 항목입니다.

A) **이번엔 미포함 (권장) — probe 표면만** — fixture는 이번 증분 목표인 probe 6개(또는 5개) positive 경로에 집중. observation 표면은 well-known provider 배선 증분에서 함께 추가. 스코프 응집·정직성 유지.

B) **표면을 미리 심어두되 dormant** — observation 취약 표면(robots/.env/.git/crossdomain/backup 등)을 fixture에 미리 배치. 지금은 finding 미발생(정직하게 문서화), 이후 provider 배선 시 즉시 재사용.

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: B
