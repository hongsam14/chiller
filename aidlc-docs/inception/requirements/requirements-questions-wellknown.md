# Requirements Questions — Observation well-known-path GET provider (r10)

**문제**: observation 시그니처는 URL-scoped라 **제출된 URL의 응답**(`ctx.Response`)만 판정합니다. 그래서 well-known 아티팩트를 요구하는 항목이 스캔 시 아티팩트를 **한번도 받지 못해** false-clean/N-A가 됩니다:
- **false-clean**(요청도 안 하고 "checked, 이상없음"): PS-008 robots.txt · PS-011 .env · PS-012 backup · PS-022 crossdomain.xml
- **N/A**(path-guard로 덜 유해하나 미요청): PS-010 .git · PS-013 admin

**목표**: approved 호스트에 대해 well-known 경로(`/robots.txt`·`/.env`·`/.git/config`·`/crossdomain.xml`·backup 이름 목록·`/admin`)를 **rate-budget 안에서 GET**해 소유 시그니처에 응답을 먹인다. FR-03("benign GET including well-known paths")·구동 플랜("robots.txt를 다시 요청해 매칭", "crossdomain.xml을 요청해 검사")에 정합. r9 `test/fixtures/vuln-target/`에 이 표면들이 이미 dormant로 존재 → 검증 즉시 가능.

**코드 사실(매핑 결과)**:
- `fanOut`(orchestrator): URL-scoped=URL당 1개, host-scoped=호스트당 1개(distinctHosts).
- 시그니처는 `ctx.Target.URL` path(`pathOf`)+`ctx.Response`로 판정 — well-known 응답을 `Target.URL=해당경로`로 주면 그대로 발화(gitExposure는 path에 `/.git`, admin은 `/admin` path, env/crossdomain/robots는 body만).
- Fetcher(`Fetch(ctx,scanID,Target)`)는 approval gate + per-host token bucket를 매 호출 적용 → 합성 Target을 넣어도 동일 예산·게이트로 GET 가능.
- coverage 회계는 checker 소유권+scope 기준(WorkItem 수 무관). URL-scoped로 넣으면 기존 `observation:<CWE>` checker에 귀속.

각 `[Answer]:` 태그 뒤에 알파벳으로 답해 주세요.

---

## Question 1 — plug-in 메커니즘 (핵심 설계)
well-known 응답을 소유 시그니처에 어떻게 전달할까요?

A) **fan-out에서 well-known URL-scoped WorkItem 합성 (권장)** — distinctHosts별로 `/robots.txt`·`/.env`·`/.git/config`·`/crossdomain.xml`·backup 목록·`/admin` Target을 **URL-scoped WorkItem으로 추가**. 기존 exec 파이프라인(fetch+gate+rate+signatures)이 그대로 판정. 시그니처 로직 거의 불변, **호스트당 1회**(distinct dedup 자연), coverage 의미 유지(URL-scoped observation checker에 귀속). 신규 컴포넌트 = well-known Target 생성기 + fanOut 확장.

B) **host-scoped WellKnownProvider 컴포넌트** — 호스트당 1회 sweep, 자체 fetch 후 시그니처 로직 실행(probe와 동형). 단 well-known CWE 소유권/등록을 provider로 재구성 필요 → 26-파티션 회계 손대야 함(리스크↑).

C) **exec.Run URL-branch 내부 sweep + per-host dedup 가드** — 신규 WorkItem 없음, coverage 불변. 단 exec은 URL당 실행되므로 동시 워커 간 "이 호스트 이미 sweep함" 공유 상태 필요(동시성 복잡).

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A

---

## Question 2 — 대상 well-known 경로 집합 + 소유 항목
어느 항목까지 이번 증분에 배선할까요?

A) **6개 항목 전부 (권장)** — PS-008 robots.txt · PS-011 .env · PS-010 .git/config · PS-022 crossdomain.xml · PS-012 backup(작은 고정 이름 목록) · PS-013 admin(작은 고정 경로 목록). backup 예: `backup.zip`,`www.zip`,`db.sql`,`index.php.bak`,`.env.bak` 등 소수; admin 예: `/admin`,`/administrator`,`/wp-admin`,`/manage`.

B) **false-clean 4개만** — robots/.env/backup/crossdomain(가장 유해한 false-clean)만 배선, .git/admin(현재 N/A)은 다음 증분.

C) 사용자 지정 목록 (아래 설명)

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A

---

## Question 3 — 트리거/안전 (언제 sweep?)
well-known sweep을 언제 수행할까요?

A) **approved 호스트 전부, 기본 on (권장)** — 스코프 내 호스트에 per-host 1회, rate-budget·approval gate 안에서. FR-03 정합. (기존 fetch가 이미 gate+rate 적용 → 신뢰경계 유지.)

B) **opt-in ScanOption 플래그** — `ScanOptions`에 `WellKnown bool` 추가, 켠 스캔에서만.

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A

---

## Question 4 — 시그니처 사전/값 정합성(P1) 포함 여부
Backlog P1(얕은 사전: `.env` 4키뿐, backup 이름 heuristic, `.git` config/HEAD만 등)도 이번에?

A) **배선만 — dictionary 심화는 별개 증분 (권장)** — 이번 증분은 **아티팩트 전달**에 집중(well-known GET→시그니처). `.env` 키셋 확장·backup 이름 heuristic·`.git` 추가 파일 등 P1 값 정합성은 후속 증분. 스코프 응집·리스크↓.

B) **배선 + P1 dictionary 심화 함께** — 전달과 동시에 시그니처 사전도 넓힘(범위↑).

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: B

---

## Question 5 — 검증/골든 (r9 fixture 재사용)
positive 검증을 어떻게?

A) **r9 vuln-target fixture dormant 표면 활성화 + in-image 통합 positive (권장)** — fixture에 이미 있는 robots/.env/.git/crossdomain/backup 표면의 "dormant, no-finding" 주석을 제거하고, well-known sweep이 **실제 finding**을 내는지 유닛(합성 경로)+통합으로 단언. r9와 동일 정직성.

B) **유닛 테스트만** — 합성 ScanContext 유닛 단언, 이미지 통합 positive는 이월.

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A

---

## Question 6 — MaxURLs/rate budget 상호작용
합성 well-known 항목이 사용자 `MaxURLs`·rate와 어떻게 상호작용할까요?

A) **MaxURLs 카운트 제외, per-host rate budget은 공유 (권장)** — well-known은 엔진 파생이라 사용자 URL 상한(SR-1)과 무관하게 추가하되, per-host rate(SR-4)·approval(SR-3)은 그대로 준수. (사용자가 넣은 URL 수 제한을 엔진 내부 GET이 잡아먹지 않음.)

B) **MaxURLs에도 포함** — 합성 항목도 상한에 카운트(상한 근처 스캔에서 잘릴 수 있음).

X) 기타 (아래 [Answer]: 태그 뒤에 설명)

[Answer]: A
