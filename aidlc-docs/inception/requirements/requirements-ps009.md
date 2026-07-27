# Requirements — Observation 오탐 저감: PS-009 정규식 + 구조적 증폭 제거

**Increment**: r16 · Depth: Standard → **Expanded** (구조 포함) · Type: Brownfield (signature-quality)

## 0. 진단 (실측 + 코드 매칭, 2026-07-27)

실측(kbland.kr): **15 finding 중 13(87%)이 PS-009 오탐** — 전부 동일 jQuery 압축코드
(`q,{radio:!0,...password:!0...}`). 제출 URL 1 + well-known 12 경로에서 같은 JS가 13회 발화.
나머지 2(humble HSTS·클릭재킹)만 실제.

**근본원인 = 4단 증폭 체인** (PS-009 정규식 하나만 고쳐선 부분 해결):
1. **per-hit FP**: `secretAnchor`의 `password\s*[=:]`·`api[_-]?key`·`secret[_-]?key`가
   minified JS에 매칭. JS 브랜치가 압축본문 **전체**를 스캔. (`signatures.go:116, 132-139`)
2. **증폭A**: well-known 합성 항목이 소유 시그니처가 아니라 **관측 13종 전부** 실행 →
   PS-009/018/023/쿠키/plaintext가 모든 well-known 경로에서 발화. (`exec.go:88-90`)
3. **증폭B**: well-known 항목마다 같은 first-party JS를 재발견·재스캔(soft-404가 홈 HTML
   반환). (`exec.go:63-78`, `discover.go`)
4. **증폭C**: dedup 키 = CWE+**TargetURL**+tool (증적 미포함) → URL 다른 13개 미병합.
   (`finding.go:104-112`, `dedup.go`)

**결론**: PS-009 정규식(FR-1~5)만으론 이번 증상은 가려도 구조적 증폭기(A/B/C)가 남아
PS-023/003/013/쿠키로 재발. 스코프 확장 결정(2026-07-27): **정규식 + 구조(FR-6/7)**.

## 0.2 갭 분석 — 사람이 찾은 실취약점 대조 (ground truth, 2026-07-27)

입력: `kbland-v1.html`(OFFen RED DAST, 사람 발견 16건). 실제 URL을 직접 제출해 실증
(발견-갭 vs 시그니처-갭 분리):

- **[지배적] 발견/스코프 갭**: 16건 중 10+건이 `api.kbland.kr`(미스캔 서브도메인)·깊은
  엔드포인트(`/share/page/dologin`, `/callback/`, `/v3/api-docs`). 우리는 제출 홈+well-known만
  봄. **실증**: 그 URL 직접 주니 쿠키(CWE-614/1004/1275)·CORS(corsy CWE-346)·헤더(humble)를
  전부 잡음 → 체커는 정상, **거기까지 못 감**이 원인. → 최대 recall 레버 = **크롤러 +
  서브도메인 스코프** (별도 큰 증분, §4 Playwright/asset-discovery와 연결). 부수: API DTO가
  DisableWellKnown 미전달(off-switch 미노출).
- **[시그니처 갭 — r16에 포함]**: 가리켜도 못 잡은 것 →
  - PS-018 `accessToken=`/`token=` 미포함(human #3, CWE-598) — FR-8.
  - PS-013 `/v3/api-docs`·swagger 미포함(human #10, CWE-284) — FR-9.
  - PS-009 실토큰(`eyJ…`)·내부호스트(`stage-www`) 놓치고 jQuery 오탐만(human #15/#16) — FR-1~3.
- **[설계상 능동/위임]**: XSS(#4)·접근통제(#5)·리플레이(#6)·스택트레이스(#7/#11)·API로직(#14)
  = passive 밖(능동/Nuclei). 우리 실패 아님.

**스코프 결정(2026-07-27)**: r16 = PS-009(정규식+구조) + **값싼 시그니처 승리 PS-018/PS-013**.
발견/크롤·서브도메인 스코프는 **별도 큰 증분으로 명시 이월**(Backlog TOP).

---

**Owner checker**: `observation:CWE-615` (`sensitiveCommentSig`), `internal/scan/checking/observation/signatures.go`

## 1. Intent (의도 분석)

PS-009는 HTML 주석 + 배포된 JS 소스에서 민감정보(토큰/키/비밀)를 탐지한다. 현재
`secretAnchor` 정규식이 두 방향으로 부실하다:

- **오탐(FP)**: `password\s*[=:]`가 너무 느슨해 **minified JS**의 비-비밀 코드에 발화한다.
  r15 실데이터 검증에서 kbland.kr의 PS-009 finding이 jQuery 압축코드
  `...password:!0,...`(불리언 프로퍼티)를 "민감 토큰"으로 오탐함이 드러남.
- **미탐(FN)**: 현대 고신호 토큰 포맷을 전혀 잡지 못한다 — `AKIA…`(AWS),
  `ghp_…`(GitHub), `sk_live_…`(Stripe), `AIza…`(Google), `xoxb-…`(Slack),
  JWT `eyJ…`. 또 **JSON형** `"password": "…"`은 사이 `"` 때문에 현재 정규식이 놓침
  (JS-객체형 `password:"…"`만 매칭). ~7 패턴 vs 업계 도구 100+.

## 2. Functional Requirements

**참조 원칙 (개정, 2026-07-28) — 카테고리 내 comprehensive**: PS-009의 시크릿 정규식은 손으로
만들지 않고 **검증된 OSS 룰셋을 참조·적응**한다 — 채택: **gitleaks** (`config/gitleaks.toml`,
**MIT**, 160+ 규칙). passive는 비용이 거의 0(이미 받은 바이트에 RE2 선형)이라 **패턴을 줄이지
않는다**: 항목(시크릿)이 맞으면 **RE2 이식 가능한 규칙을 전량** 적응한다. **FP는 패턴을 빼서가
아니라 각 규칙의 엔트로피 게이트 + allowlist + 앵커로 제어**한다(그래서 규칙별 엔트로피 임계·
allowlist도 함께 이식). trufflehog=AGPL(코드복사 회피); ZAP=Apache(FR-8용, 시크릿엔 부적합).
출처는 코드 주석 + 커밋에 attribution.

**정직성 제약(RE2)**: gitleaks 일부 규칙은 PCRE(백레퍼런스/lookahead)라 Go RE2로 그대로 이식
불가. 등가 변환하거나, 불가하면 **조용히 빼지 않고 "이식 불가"로 명시 로그/문서화**한다.

> **⚠️ "증류(distill)"이지 "사용(use)"이 아님 (전 참조 원천 공통)**: gitleaks·**ZAP**·**Nuclei**는
> **저작(authoring) 시점에 그 레포의 시그니처 데이터(정규식/파라미터목록/경로+매처)를 뽑아 우리
> 관측 엔진의 Go RE2 시그니처로 이식**하는 참조일 뿐이다. **런타임에 이 툴들을 실행하지 않는다** —
> 바이너리 의존·파일 벤더링·엔진 호출 전부 없음. 특히 **Nuclei는 우리의 위임 엔진(PS-005/006/026
> delegated)과 별개**다: FR-9는 Nuclei 엔진에 위임하거나 호출하는 게 아니라, api-doc **탐지 패턴만**
> 증류해 passive PS-013(소유 CWE 불변)에 넣는다. 즉 결과물은 `signatures.go` 안의 자립형
> RE2 시그니처이며 외부 룰셋 덤프·런타임 결합이 아니다.

- **FR-1 (FP 축소 — 엔트로피 게이트)**: 키워드(`password`/`secret` 등) 매칭은 **그럴듯한
  비밀 값**을 동반할 때만 발화한다 — gitleaks식으로 (a) 값의 **Shannon 엔트로피 임계**를
  넘고, (b) JS-리터럴(`:!0`/`:true`/`:false`/`:function`/`:{`/`:[`)·문서-예시 allowlist가
  아닐 때. 이 엔트로피 게이트가 minified JS(`password:!0`, `apiKey:` 프로퍼티)의 저엔트로피
  매칭을 죽인다.
- **FR-2 (FN 확장 — gitleaks 룰셋 전량 적응)**: gitleaks의 **RE2 이식 가능한 시크릿 규칙을
  전량** 적응한다(카테고리 내 comprehensive) — AWS/GCP/Azure, GitHub/GitLab, Stripe, Google,
  Slack, Twilio/SendGrid/Mailgun, npm/PyPI/Docker, JWT `eyJ….eyJ….`, PEM PRIVATE KEY 등
  `config/gitleaks.toml`의 전체 룰. 각 규칙은 **엔트로피 임계 + allowlist(문서 예시
  `AKIAIOSFODNN7EXAMPLE` 등)를 함께** 이식해 FP를 제어(패턴 축소로가 아님). 데이터 테이블로
  두어 유지비 최소화. RE2 미지원 규칙은 등가 변환 또는 명시적 skip 로그.
- **FR-3 (FN 확장 — JSON형)**: `"(password|secret|api[_-]?key|token)"\s*:\s*"<value>"`
  형태를 매칭(현재 놓치는 JSON 설정/응답).
- **FR-4 (증적 불변)**: r15의 실-매칭-데이터 증적을 유지 — 매칭된 실제 토큰/값 +
  주변 맥락을 bounded(≤200)로 담는다.
- **FR-5 (탐지 표면 불변)**: 여전히 HTML **주석** + **배포 JS**만 스캔(가시 HTML
  본문 제외) — 로그인 라벨 "Password:" 오탐 방지 규율 유지.

### 구조적 증폭 제거 (신규, 확장 스코프)

- **FR-6 (증폭A 제거 — well-known 소유-시그니처-만)**: well-known 합성 항목은 **그 경로를
  소유한 시그니처만** 실행한다 (robots→PS-008, .env→PS-011, .git→PS-010, crossdomain→
  PS-022, backup→PS-012, admin→PS-013). 경로-비의존 body/JS 스캐너(PS-009 comment,
  PS-003 plaintext, PS-023 captcha, PS-015/016/017 쿠키)는 well-known 경로에서 실행하지
  않는다. → well-known 스윕이 소유 시그니처 외의 오탐을 ~14× 증폭하던 것을 제거.
  구현 힌트: well-known path 목록을 path→owning-CWE로 만들고 WorkItem이 소유 CWE를 실어
  exec가 그 시그니처만 돌린다. (**26 파티션·소유권 불변** — 비소유 시그니처는 제출 URL에서
  이미 실행되므로 커버리지 무영향; r10 fold의 ranCheckers도 이에 맞춰 갱신.)
- **FR-7 (자산/증적 중복 제거) — 이월(2026-07-28)**: 코드 규약 리뷰 결과 dedup 계약(IdentityKey-only,
  P2 멱등) 위반이라 이월. FR-6가 관측된 증폭을 이미 제거하므로 r16 불필요. Backlog: exec/discovery
  계층 JS-자산 dedup(계약 보존, 큰 변경).

### human-found 갭 폐쇄 — **OSS 시그니처 참조** (손수 금지)

값싸다는 이유로 손으로 만들면 또 FN/FP를 낳는다. FR-8/9도 **검증된 OSS 규칙을 참조·적응**한다.

- **FR-8 (PS-018 URL 민감정보 — OWASP ZAP 참조)**: `sessionInURLSig`를 **ZAP
  `InformationDisclosureInUrlScanRule`(Alert 10024, Apache-2.0)** 모델로 적응한다 — ZAP의
  큐레이트 파라미터명 목록(`user·username·pass·password·pwd·token·ticket·session·jsessionid·
  sessionid`; `token`이 substring이라 `accessToken`/`access_token`/`id_token` 포함) + ZAP의
  값-패턴 정규식(신용카드·이메일·SSN 등). **손수 추가한 `auth`/`bearer` 같은 FP 위험 항목은
  ZAP 목록으로 대체.** human #3(`/callback/?accessToken=eyJ…`, CWE-598) 폐쇄. Go RE2 이식.
- **FR-9 (PS-013 노출 API/관리 표면 — Nuclei 참조)**: `adminPageSig`에 **노출된 API 문서·관리
  표면** 탐지를 **Nuclei `http/exposures/apis/`(MIT) + 관련 `exposures/configs/`** 를 참조해
  추가한다 — 경로(swagger/openapi/redoc/wadl/wsdl/graphql/actuator 등) **+ Nuclei의 본문 매처**
  (`"swagger":`/`"openapi":`/graphql introspection/actuator JSON)로 **경로만-매칭 FP를 회피**.
  adminContentMarker는 이 표면에 안 맞으므로 **api-docs 전용 본문 확인 분기**를 둔다(소유 CWE는
  PS-013 유지 → 26 파티션 불변). human #10(`api.kbland.kr/v3/api-docs`, CWE-284) 폐쇄.
- **FR-10 (PS-009 내부/인프라 노출 — ZAP Private IP Disclosure 참조)** [갭 B, 이번 포함]:
  배포된 JS/주석에 **내부 인프라 정보가 노출**되는 것을 잡는다.
  - **주(distill, 저-FP)**: ZAP **Private IP Disclosure**(Alert 2, CWE-200, Apache-2.0) 모델 —
    RFC1918 IPv4(10./172.16-31./192.168.) + AWS/EC2 private 호스트명(`ip-10-…`, `*.compute.internal`,
    `*.ec2.internal`). ZAP의 알려진 FP(버전 문자열 `vp09.02.51.10…`)는 옥텟 범위 검사 + 워드 경계로 가드.
  - **부(heuristic, 명시 라벨)**: 비-프로덕션 호스트 마커 — `stage[-.]`/`staging[-.]`/`dev[-.]`/
    `qa[-.]`/`uat[-.]` 접두 또는 `.local`/`.internal`/`.corp` 접미의 호스트명. human #16
    (`stage-www.kbland.kr`) 폐쇄. FP 가드: 호스트명 컨텍스트(URL/도메인 형태)에 앵커, 임의 부분문자열 금지.
  - 소유 CWE는 **PS-009(CWE-615) 유지**(배포 소스 내 민감정보) → 26 파티션 불변.

## 3. Non-Functional (활성 extension)

- **security-baseline**: ReDoS 불가(RE2 선형 — 구조상 보장, "하드닝" 불요). 증적은
  bounded(FR-12)+암호화저장(SECURITY-01) 유지.
- **property-based-testing (FULL)**: PBT — finding은 정확히 소유 CWE(CWE-615)만,
  증적은 항상 bounded.
- **resiliency-baseline**: 해당 없음(순수 in-process 정규식, I/O 없음).

## 4. Scope Boundary (정직)

- **IN**: PS-009 정규식+구조(FR-1~7, gitleaks 참조) · PS-018 URL 민감정보(FR-8, ZAP 참조)
  · PS-013 노출 API 표면(FR-9, Nuclei 참조) · PS-009 내부/인프라 노출(FR-10, ZAP Private IP Disclosure 참조 + 비-prod 호스트 휴리스틱, 갭 B 포함).
- **OUT (명시 이월, Backlog TOP)**: **[갭 A, 지배적] 크롤러 + 서브도메인 스코프**(최대 recall
  레버, §4 Playwright/asset-discovery) — r16 후에도 단일 URL 제출 시 api.kbland.kr·깊은
  엔드포인트 10+건 미탐 · API DisableWellKnown off-switch 미노출 · PS-011 .env 값 엔트로피 · P0
  transport(br/zstd) · 능동(XSS/BAC/replay/stacktrace)=Nuclei/AI 위임.
- **불변**: 카운트·소유권·26 파티션·인터페이스 (시그니처 내부 + well-known 스코핑만).

## 5. Verification (r10 패턴 계승 + ground-truth 회귀)

- **FP/FN 코퍼스(blocking)**: minified-JS FP(`password:!0`) + gitleaks 토큰 포맷 FN + JSON형 +
  ZAP URL-param FN/FP + Nuclei api-docs FN/FP + 기존 정탐 회귀 — 각 verdict 단언.
- **구조 회귀**: well-known 소유-시그니처-only(FR-6) + 자산/증적 중복제거(FR-7).
- **ground-truth 회귀**: human #3(accessToken URL)·#10(v3/api-docs)·#15(JS 내 실토큰)·#16(내부 호스트 stage-www)이
  올바른 verdict로 잡히고 jQuery FP는 0.
- PBT + 로컬 게이트(build/vet/gofmt/arch-check/race/gosec).

## 6. 명확화 (Clarification) — ✅ RESOLVED 2026-07-27

- **Q1 = C**: 값 동반 필수 + JS-리터럴 가드 (가장 강한 FP 차단).
- **Q2 = A**: 큐레이트 고신호 토큰 셋(AWS/GitHub/Stripe/Google/Slack/JWT/PEM). 제네릭 엔트로피 휴리스틱은 이월.
- **Q3 = A**: FP/FN 코퍼스(blocking) + PBT.
- **Q4 = A (+ 의존성 단서)**: 새 브랜치. **단 main엔 r10(PR #13)·r15(PR #20)의 signatures.go 변경이 아직 없음** → main에서 바로 브랜치하면 stale. 해소책은 Workflow Planning에서 확정(권장: `feat/compliance-report`에서 브랜치해 두 선행 변경을 포함, PR은 #20 스택 또는 #13/#20 병합 후 재베이스).

### (이월) 다음 todo
- 제네릭 엔트로피 휴리스틱(Q2=B) — `KEY=<40+자 base64/hex>`.
- PS-011 .env 값 엔트로피, 기타 시그니처.

## 7. References — gitleaks {#references-gitleaks}

- **gitleaks** — MIT. 시크릿 정규식 참조 원천(160+ 규칙, 정규식+엔트로피+allowlist).
  - 룰셋: https://github.com/gitleaks/gitleaks/blob/master/config/gitleaks.toml
  - 리포: https://github.com/gitleaks/gitleaks
  - Code Generation 단계에서 `config/gitleaks.toml`을 WebFetch로 받아 필요한 고신호
    규칙(AWS/GitHub/Stripe/Google/Slack/JWT/PEM)을 Go RE2로 이식 + attribution.
- trufflehog — AGPL-3.0 (코드 복사 회피). OWASP ZAP — Apache-2.0 (능동·헤더 위주, 시크릿엔 부적합).
- **OWASP ZAP** — Apache-2.0. FR-8 참조(URL 민감정보). `InformationDisclosureInUrlScanRule`
  (Alert 10024) + 파라미터 목록 `URL-information-disclosure-messages.txt`(user/username/pass/
  password/pwd/token/ticket/session/jsessionid/sessionid) + 값-패턴 정규식.
  - https://www.zaproxy.org/docs/desktop/addons/passive-scan-rules/
  - https://github.com/zaproxy/zap-extensions (addOns/pscanrules)
- **Nuclei / nuclei-templates** — MIT. FR-9 참조(노출 API/관리 표면). `http/exposures/apis/`
  (swagger-api·openapi·redoc-api-docs·wadl-api·wsdl-api·asyncapi …) + `exposures/configs/`
  (graphql·actuator). 경로 + 본문 매처.
  - https://github.com/projectdiscovery/nuclei-templates/tree/main/http/exposures/apis
- **OWASP ZAP — Private IP Disclosure** (Alert 2, CWE-200, Apache-2.0). FR-10 참조(내부 IP/EC2 호스트).
  - https://www.zaproxy.org/docs/alerts/2/
