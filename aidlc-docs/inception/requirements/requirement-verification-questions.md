# 요구사항 명확화 질문 — recon 노하우 → observation 반영 (r21)

OffenRed-AI-DAST의 passive/recon 소스에서 노하우를 추출했습니다. 아래는 추출 결과와, chiller **불변 규율**(26-파티션 · checker↔CWE 소유권 · OSS-증류/손수정규식 금지 · RE2 · FR-6 소유-시그니처-only) 안에서 반영 범위를 정하기 위한 질문입니다. 각 `[Answer]:` 뒤에 선택지 문자(A/B/…)를 채워 주세요.

---

## 추출한 노하우 요약 (근거)

| # | recon 노하우 | 출처 파일 | 후보 KISA 항목 | 현재 소유 |
|---|---|---|---|---|
| K1 | **백업 파일 본문 매직 검증** (`PK`=zip, `SQLite`, `-- MySQL/PostgreSQL dump`) — 이름뿐 아니라 실제 아카이브인지 본문으로 확인 | `stage2c_passive.py:_verify` | **PS-012** | observation ✅ |
| K2 | **관리/디스클로저 제품 콘텐츠 마커** — Spring Actuator(`activeProfiles`/`propertySources`/`_links`), phpinfo(`phpinfo()`), H2 콘솔, Apache `server-status`/`server-info` | `stage2c_passive.py:_verify` | **PS-013** | observation ✅ |
| K3 | **추가 시크릿 패턴** — Stripe `sk_live/sk_test`, Slack `xox*`, GitHub `gh[pousr]_`, npm_, JWT `eyJ…`, AWS `AKIA` | `stage2c_passive_sensitive_agent.py` | **PS-009** | observation ✅ |
| K4 | **에러/스택트레이스/디버그 콘솔 본문 마커** — `traceback`/`werkzeug`/debug toolbar(djdt)/ELMAH/interactive console | `stage2c_passive_no_surface_agent.py`, `response_signals.py` | **PS-005·PS-006** | **Nuclei(위임)** ⚠️ |
| K5 | **개인정보(PII) 응답 본문 노출** — 한국 주민번호 `\d{6}-[1-4]\d{6}`, email/phone/address JSON 필드, 여권/국적 ID | `stage2c_passive_sensitive_agent.py` | **KISA 26에 항목 없음** ⚠️ |
| K6 | **`.DS_Store` 본문 매직**(`Bud1`) — 디렉터리 구조 소스 디스클로저 | `stage2c_passive.py:_verify` | 명확한 소유 없음(소스 노출 계열) ⚠️ |
| K7 | 디렉터리 목록 본문 증명(`directory_listing_body_proof`) | `stage2c_passive_no_surface_agent.py` | **PS-004** | **dirhunt(probe)** ⚠️ |

**핵심 판단**: K1·K2·K3은 observation 소유 항목의 **정합성 강화**(불변 유지). K4~K7은 소유권/26-파티션 경계를 건드리므로 아래에서 결정이 필요합니다.

---

## Question 1
이번 증분의 **핵심 범위**를 어디까지로 할까요? (observation 소유 항목의 in-partition 강화 = 불변 유지)

A) **K1+K2+K3 전부** — PS-012 백업 매직 + PS-013 관리/디스클로저 마커 + PS-009 시크릿 패턴 보강 (관측엔진 3개 시그니처 정합성 강화, 26-파티션·소유권 불변)

B) K1+K2만 (백업 매직 + 관리 마커; 시크릿은 이미 gitleaks-증류로 충분하다고 보고 제외)

C) K1만 (백업 파일 본문 매직 검증만, 최소 범위)

D) Other (please describe after [Answer]: tag below)

[Answer]: A

## Question 2
**K3(추가 시크릿 패턴)** 처리 방식. 현재 PS-009는 gitleaks-증류 규칙(`configs/signatures/secrets.yaml`)을 씁니다. recon의 Stripe/Slack/GitHub/npm/JWT/AWS 패턴은 대부분 gitleaks에 이미 있을 가능성이 큽니다.

A) **갭만 증류 추가** — recon 패턴을 gitleaks 규칙집합과 대조해 **누락된 것만** OSS-증류 규율로 `secrets.yaml`에 추가(중복이면 no-op). 손수 정규식 금지 유지.

B) 대조 없이 recon 패턴을 그대로 추가 (중복 감수)

C) K3 제외 (Q1에서 A/C 선택 시 무관)

D) Other (please describe after [Answer]: tag below)

[Answer]: A

## Question 3
**K4(에러/스택트레이스/디버그 디스클로저)** — PS-005(오류 페이지)·PS-006(스택 트레이스)는 현재 **Nuclei 위임**입니다. recon은 이를 in-process로 판정할 강한 본문 마커를 제공합니다.

A) **위임 유지 (이번 증분 제외)** — 26-파티션·위임 경계 불변. Nuclei가 계속 소유. (가장 보수적)

B) **PS-005/006을 observation-native로 승격** — 과거 6개 네이티브 시그니처 승격(C3=A)처럼 위임 4→2, observation 13→15로 파티션 변경. 별도 CWE 소유(예: CWE-209 정보노출/스택트레이스) 신규 지정 필요.

C) 이번 증분은 Nuclei 위임 유지하되, recon 마커를 **후속 증분 후보로만 기록**(코드 변경 없음)

D) Other (please describe after [Answer]: tag below)

[Answer]: B. nuclei-template github 레포지토리 `https://github.com/projectdiscovery/nuclei-templates`에서 관련 시그니처를 추출해서 사용. (이미 우린 그런 적 있음)

## Question 4
**K5(개인정보 PII 노출)** — KISA 26 항목에 대응 슬롯이 **없습니다**(주민번호/이메일/전화 노출은 26 목록 밖). r20 evidence 강화 때 "고객 UI가 아니라 다운스트림 AI 근거"로 스코프를 정리한 맥락이 있습니다.

A) **이번 증분 제외** — 26-파티션 순수성 유지. 필요 시 별도 KISA-확장 증분에서 다룸.

B) **KISA-26 밖 별도 카테고리로 신규 도입** — observation에 PII 시그니처 추가하되 26 커버리지 회계와 분리(예: CWE-359, `PS-` 아닌 별도 네임스페이스). 파티션 회계에는 미포함.

C) Other (please describe after [Answer]: tag below)

[Answer]: B. CWE로 분류하면 `coverage/mapping`에서 매핑 시키면 될 문제.

## Question 5
**K6(.DS_Store)·K7(디렉터리 목록)** 처리.

A) **둘 다 제외** — .DS_Store는 명확한 PS 소유가 없고, 디렉터리 목록(PS-004)은 dirhunt(probe) 소유라 observation이 침범하지 않음. (소유권 불변)

B) `.DS_Store`만 **PS-010(.git 계열 소스 노출)** 소유 하에 well-known + 매직 검증으로 추가; 디렉터리 목록은 dirhunt 유지

C) Other (please describe after [Answer]: tag below)

[Answer]: B

## Question 6
**K2 관리/디스클로저 마커의 well-known 경로**를 함께 추가할까요? (현재 well-known에 `/actuator`는 있으나 `/actuator/env`·`/phpinfo.php`·`/h2-console`·`/server-status`는 없음)

A) **콘텐츠 마커 + well-known 경로 모두 추가** — `/actuator/env`, `/phpinfo.php`, `/h2-console`, `/server-status`, `/server-info`를 PS-013 소유로 well-known에 추가(FR-6 소유-only 유지, approved-host+rate 게이트 상속)

B) **콘텐츠 마커만 강화**, well-known 경로는 추가하지 않음(제출된 URL이 해당 경로일 때만 판정)

C) Other (please describe after [Answer]: tag below)

[Answer]: A

## Question 7
검증(테스트) 수준. 이 프로젝트는 property-based testing가 FULL로 강제됩니다.

A) **FP/FN 코퍼스 + PBT + 통합 재확인** — 각 강화 시그니처에 대해 (i) 실제 아티팩트(진짜 백업 매직/actuator env) 양성 케이스 (ii) 오탐 유발 케이스(soft-404, 버전 문자열 등) 음성 케이스 코퍼스 + PBT(경계·소유-only·매직 판별) + 26-파티션/소유권 불변 재확인. (기존 증분과 동일 강도)

B) 단위 테스트만 (코퍼스/PBT 최소화)

C) Other (please describe after [Answer]: tag below)

[Answer]: A

---

## Extension 유지 확인 (opt-in 캐리포워드)
현재 `aidlc-state.md`에 3개 확장이 **Enabled=Yes**로 기록되어 있습니다. 이번 증분에도 그대로 강제 적용됩니다(변경을 원하면 아래에 표기).

## Question 8
security-baseline · resiliency-baseline · property-based-testing 확장을 이번 증분에도 **모두 활성(강제)** 으로 유지할까요?

A) **모두 유지** (기존과 동일, 권장)

B) 일부 조정 (아래에 어떤 확장을 어떻게 바꿀지 기술)

C) Other (please describe after [Answer]: tag below)

[Answer]: A
