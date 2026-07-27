# Requirements Clarification — Q4=B dictionary 심화 범위 (well-known provider, r10)

확정: **Q1=A(fan-out 합성) · Q2=A(6개 항목) · Q3=A(기본 on) · Q5=A(fixture positive) · Q6=A(MaxURLs 제외).**

Q4=**B**(P1 dictionary 심화를 이번 증분에 함께)로 스코프가 넓어졌습니다. 다만 "P1 dictionary 심화"의 **범위**와 **성격**을 확정해야 합니다. Backlog P1은 항목별로 FN(놓침) 확장과 FP(오탐) 축소가 **짝**으로 걸려 있습니다.

각 `[Answer]:` 태그 뒤에 알파벳으로 답해 주세요.

---

## Clarification 1: dictionary 심화 대상 범위
어느 항목의 사전을 이번 증분에 심화할까요?

A) **이번에 배선하는 well-known 6개 항목만 (권장)** — PS-008 robots · PS-011 .env · PS-012 backup · PS-022 crossdomain · PS-010 .git · PS-013 admin. 배선과 **같은 항목**이라 응집도 높음. 구체:
   - PS-011 `.env`: 키셋 확장(`JWT_SECRET`/`MAIL_PASSWORD`/`STRIPE_SECRET_KEY` 등 접두 포함) + `KEY=value` 형태/엔트로피
   - PS-012 backup: 확장자만이 아니라 backup-name heuristic(`index.php.bak`,`db.sql`,`www.zip`)
   - PS-010 `.git`: `config`+`HEAD`+directory-listing/`index`/`packed-refs`
   - PS-013 admin: 목록 확장(`/manager/html`,`/phpmyadmin`,`/dashboard`,`/backend`)
   - PS-008 robots: 키워드 확장(`/database`,`/sql`,`/.env`,`/logs`)
   - PS-022 crossdomain: 견고(추가 少), `clientaccesspolicy.xml`은 배선 대상 밖

B) **P1 전체 항목** — 위 6개 + **well-known이 아닌** PS-009 comment(토큰포맷/JSON `"key":"value"`) + PS-018 session `sid` + PS-023 captcha. 범위 최대(배선 항목 밖까지 사전 손봄).

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A. B는 다음 작업 todo로.

---

## Clarification 2: FP-tightening 포함 (backlog는 FN확장 ↔ FP축소 짝)
사전을 넓히면 신규 FP 위험이 커집니다. Backlog P1은 짝지어 FP 축소도 요구합니다(예: `.env` 프레임워크 doc FP 가드, 정상 아카이브 backup FP, 광역 `sid` FP, admin SPA FP). 포함할까요?

A) **FN 확장 + FP 축소 둘 다 (권장)** — 사전을 넓히되 동시에 loose anchor를 조여 신규 FP 방지. Backlog acceptance 그대로: **FP/FN 코퍼스**(정상 아카이브·프레임워크 `.env` doc·JSON secret·SPA 로그인 등)로 각 verdict를 단언. 안전한 심화.

B) **FN 확장만** — 놓침만 줄이고 FP 축소는 후속(신규 FP 감수).

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A

---

## Clarification 3: P0 transport 이슈는 이번 스코프 밖 확인
Backlog P0(br/zstd 바디 미디코드·512KB 컷·`hasPasswordField` SPA)는 **transport 정확성**이지 dictionary가 아닙니다. 이번 증분(배선 + dictionary)에는 **미포함**이 맞나요?

A) **미포함 동의 (권장)** — P0 transport는 별개 증분(정합성 correctness). 이번은 well-known 배선 + P1 dictionary.

B) **P0도 포함** (아래 설명 — 범위 대폭↑)

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A
