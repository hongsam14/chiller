# Requirements — recon 노하우 → observation 반영 (r21)

## Intent Analysis
- **User request**: OffenRed-AI-DAST(`../OffenRed-AI-DAST/stages/`)의 passive/recon 노하우를 추출해 chiller의 **observation** 엔진에 반영.
- **Request type**: Enhancement (brownfield 증분) — 검출 커버리지·정합성 향상.
- **Scope**: Multiple components — `internal/scan/checking/observation/` + `configs/signatures/` + `configs/coverage/kisa.yaml` + `configs/mappings/kisa.yaml` + 신규 `third_party/`(nuclei-templates 증류 원본) + 코퍼스/PBT 테스트.
- **Complexity**: Moderate–Complex — 3개 in-partition 강화(불변) + **파티션 변경 3건**(PS-005/006 위임→native 승격, PII 신규 카테고리, .DS_Store 소유 편입).
- **Depth**: Standard→Comprehensive (파티션·소유권 회계를 건드리므로 불변 추적 필수).

## 확정된 답변 (requirement-verification-questions.md)
Q1=A · Q2=A · Q3=B · Q4=B · Q5=B · Q6=A · Q7=A · Q8=A.

## 불변 규율 (INVARIANTS — 전 단계 유지)
- **OSS-증류 규율**: 손수 정규식 금지. 시크릿=gitleaks, url-sensitive=ZAP, api-docs=nuclei 증류 기존 규율 그대로. **PS-005/006 신규 시그니처는 nuclei-templates(`github.com/projectdiscovery/nuclei-templates`)에서 pinned-SHA로 authoring-time 증류→RE2 포팅** (r16 gitleaks/ZAP/nuclei 증류와 동일 방식: `third_party/` pinned + `tools/sigimport` 생성기 + `make signatures`/`-check` drift 0).
- **checker↔CWE 소유권**: one owning signature per CWE, one-finding-per-CWE(R7). `.DS_Store`는 신규 시그니처가 아니라 **기존 PS-010 시그니처(CWE-527) 내부 확장**으로 처리(단일 소유 유지).
- **FR-6 소유-시그니처-only**: well-known 스윕은 소유 시그니처만 실행(FP 앰프 방지). 신규 well-known 경로도 이 규율 상속.
- **26-파티션 회계**: `configs/coverage/kisa.yaml` items 26개 목록 불변. 승격/신규 카테고리는 delegations·mappings 편집으로 처리하되 items 목록은 그대로.
- **boot totality 체크**: `coverage.go:48` — 모든 owned CWE는 `mappings.KnownCWE` 이거나 `NoCanonicalCWE`. 신규 owning CWE는 반드시 `configs/mappings/kisa.yaml`에 엔트리 추가.
- **RE2 안전성**: 모든 정규식은 Go `regexp`(RE2) — lookaround 없음, 선형 시간.

---

## Functional Requirements

### FR-1 — PS-012 백업 파일 본문 매직 검증 (K1, in-partition 강화)
현재 `backupFileSig`는 파일 **이름 휴리스틱 + not-HTML**만으로 판정. recon `_verify`의 아카이브 매직을 콘텐츠 앵커로 추가:
- **FR-1a**: 백업 아카이브 매직 인식 — `PK\x03\x04`(zip), `SQLite format 3\x00`, `-- MySQL dump`/`-- PostgreSQL`/`-- MariaDB`(SQL 덤프 헤더), gzip `\x1f\x8b`, 7z/rar 매직.
- **FR-1b**: 판정 강화 — backup-name 매치 + (매직 확인 OR not-HTML). **매직이 확인되면 High 확정**(FN 축소: 이름이 애매해도 진짜 아카이브면 발화); **이름 매치인데 HTML이면 N/A soft-404**(FP 유지). 소유 CWE-530 불변.
- **FR-1c**: 매직 확인 시 evidence Location.Offset=0 + Rule=`observation:CWE-530/backup-magic`, Hypothesis 유지.

### FR-2 — PS-013 관리/디스클로저 제품 콘텐츠 마커 + well-known (K2, Q6=A)
`adminPageSig`에 recon `_verify`의 제품 마커 추가 (콘텐츠 앵커 = soft-404 FP 차단 유지):
- **FR-2a**: 콘텐츠 마커 확장 — Spring Actuator(`activeProfiles`/`propertySources`/`_links` + `application/vnd.spring-boot` content-type), phpinfo(`phpinfo()`/`<title>phpinfo()`), H2 콘솔(`h2 console`/`h2-console`), Apache `<title>Apache Status</title>`/`Apache Server Information`. **각 마커는 nuclei-templates exposures/misconfiguration에서 증류**(손수 금지).
- **FR-2b**: well-known 경로 추가 (PS-013 소유, FR-6 소유-only 상속) — `/actuator/env`, `/actuator/health`, `/phpinfo.php`, `/h2-console`, `/server-status`, `/server-info`. approved-host + rate 게이트 상속(SEC-W2).
- **FR-2c**: 소유 CWE-306 불변. 제품 마커 확인 없이는 발화 금지(경로만으로 발화 금지 — soft-404 방어).

### FR-3 — PS-009 추가 시크릿 패턴: 갭만 증류 (K3, Q2=A)
- **FR-3a**: recon 시크릿 패턴(Stripe `sk_live/sk_test`, Slack `xox[abprs]`, GitHub `gh[pousr]_`, npm_, JWT `eyJ…`, AWS `AKIA`)을 현재 `configs/signatures/secrets.yaml`(gitleaks-증류) 규칙집합과 **대조**.
- **FR-3b**: gitleaks에 **이미 있으면 no-op**; 누락된 것만 gitleaks 규칙 형식으로 증류 추가(entropy/allowlist 포함). 대부분 이미 커버될 것으로 예상 → 실제 추가는 갭만.
- **FR-3c**: 소유 CWE-615 불변. `tools/sigimport` 재생성 멱등 + `make signatures-check` drift 0.

### FR-4 — PS-005/006 observation-native 승격 (K4, Q3=B, **파티션 변경**)
현재 Nuclei 위임. nuclei-templates에서 증류해 in-process 수동 판정으로 승격:
- **FR-4a**: **PS-006 스택 트레이스** — 응답 본문의 스택 트레이스/트레이스백 마커(`Traceback (most recent call last)`, Java `\tat pkg.Class(File.java:NN)`, `.py", line NN`, `node_modules/`, Werkzeug/debug console `djdt`/`ELMAH`/`interactive console` 등)를 nuclei-templates에서 증류. 소유 CWE 후보 **CWE-209**(FD C-5에서 핀).
- **FR-4b**: **PS-005 오류 페이지 정보 노출** — 오류 응답에 내부 경로/구성/버전이 드러나는 마커를 nuclei-templates에서 증류. 소유 CWE 후보 **CWE-497 또는 CWE-200**(PS-006와 **구분되는** 고유 CWE, FD에서 핀).
- **FR-4c**: **수동성 유지** — 능동 트리거(malformed 요청 유발) 금지. **제출/발견된 응답 본문만** 판정(관측). debug-endpoint 전용 well-known 경로는 능동-트리거 성격이라 **이번 증분 제외**(후속 후보).
- **FR-4d**: **파티션 회계 갱신** — `configs/coverage/kisa.yaml` delegations에서 **PS-005·PS-006 제거**(위임 4→2); `configs/mappings/kisa.yaml`에 두 신규 CWE 엔트리 추가(→PS-005/PS-006). observation 13→15. items 26 목록 불변.
- **FR-4e**: 소유 시그니처 2개를 observation 레지스트리(`engine.go New()`)에 등록. boot totality 통과(신규 CWE가 mappings에 존재).

### FR-5 — PII 노출 신규 카테고리 (K5, Q4=B, **26 밖**)
- **FR-5a**: 응답 본문 PII 노출 시그니처 신규 — 한국 주민번호(`\d{6}-[1-4]\d{6}`, 검증 가능한 형식 경계), email/phone/address JSON 필드, 국적/여권 ID 필드. 패턴 근거는 recon `stage2c_passive_sensitive_agent.py` + (가능 시) OSS 증류.
- **FR-5b**: 소유 CWE 후보 **CWE-359**(Exposure of Private Personal Information, FD에서 핀). **26-파티션 밖** — `configs/mappings/kisa.yaml`에 `CWE-359: { KISA: [] }`(빈 투영, subzy `no-canonical-cwe` sentinel과 동일 패턴)로 등록 → boot totality 통과 + KISA 회계 불변(Project→[]).
- **FR-5c**: 오탐 보수성 — 필드-키 기반 매칭(bare 값 매칭 금지), 주민번호는 형식 경계로 랜덤 12자리 오탐 차단. 발화 시 evidence는 매치 필드-키 컨텍스트(값 원문은 마스킹/절단 정책 검토 — FD).
- **FR-5d**: observation 레지스트리 등록. 26 items 목록·delegations 불변.

### FR-6 — .DS_Store 소스 노출 (K6, Q5=B, PS-010 내부 확장)
- **FR-6a**: 기존 `gitExposureSig`(CWE-527, PS-010)를 **소스/메타데이터 노출**로 확장해 `.DS_Store`도 인식 — 본문 매직(`Bud1` / `\x00\x00\x00\x01` 헤더) + 경로 가드(`/\.DS_Store$`). **신규 시그니처 아님**(단일 소유 CWE-527 유지).
- **FR-6b**: well-known 경로 `/.DS_Store` 추가(PS-010 소유, FR-6 소유-only 상속).
- **FR-6c**: 디렉터리 목록(K7)은 **제외** — PS-004는 dirhunt(probe) 소유, observation 침범 금지.

### FR-7 — 검증 (Q7=A)
- **FR-7a**: 각 강화/신규 시그니처에 **FP/FN 코퍼스** — (i) 진짜 아티팩트 양성(실제 zip/SQLite 매직·actuator env JSON·스택트레이스·주민번호·.DS_Store), (ii) 오탐 유발 음성(soft-404 홈페이지·버전 문자열·랜덤 12자리·라이브러리 minified).
- **FR-7b**: **PBT FULL** — 경계(offset/len), 소유-only(FR-6 well-known 스윕이 소유 시그만 실행), 매직 판별 항등, 26-파티션/소유권/boot-totality 불변.
- **FR-7c**: 통합 재확인 — 26 items·delegations(4→2)·mappings·소유권·신규dep(third_party nuclei-templates 원본만; 런타임 신규 Go dep 0 목표) 검증.

---

## Non-Functional Requirements
- **NFR-1 (security-baseline, 강제)**: nuclei-templates 증류는 authoring-time만 — 런타임 미사용. `.gitleaks.toml` allowlist에 증류 원본/코퍼스의 시크릿-형 리터럴 정확-리터럴 등재(파일 전량 스캔 유지). gosec 0, secret-scan(full-history) green.
- **NFR-2 (resiliency-baseline, 강제)**: 신규 well-known 경로는 approved-host + per-host rate budget 상속(무제한 팬아웃 금지, MaxWellKnownHosts 게이트 존중).
- **NFR-3 (property-based-testing FULL, 강제)**: FR-7b PBT 필수.
- **NFR-4**: RE2 선형 시간 — catastrophic backtracking 없음. 출력-bound evidence(maxEvidenceLen) 유지.
- **NFR-5**: 결정성 — 시그니처는 순수 함수(응답→finding), 소유-only 판정.

## Scope
**IN**: FR-1..7 (PS-012 매직 · PS-013 마커+well-known · PS-009 갭 시크릿 · PS-005/006 native 승격 · PII 신규 · .DS_Store PS-010 편입 · 코퍼스/PBT).
**OUT**: 디렉터리 목록(dirhunt 소유) · debug-endpoint 능동 트리거 well-known · discovery/서브도메인 스코프(별도 최상위 갭) · changeme(PS-019) · 능동 공격 신호(signal_patterns.py의 SQLi/LFI/SSRF/CMDI/SSTI/XSS — passive 무관).

## Open decisions → Functional Design (C-5 CWE 핀)
1. PS-005 / PS-006 owning CWE 확정 (후보: PS-006→CWE-209 스택트레이스; PS-005→CWE-497 또는 CWE-200 — **상호 구분** 필수).
2. PII owning CWE 확정 (후보: CWE-359) + PII evidence 값 마스킹/절단 정책.
3. nuclei-templates 증류 대상 템플릿 경로 셋 + pinned SHA.
4. .DS_Store를 gitExposureSig에 편입 시 시그니처 개념명/rule-id 네이밍(`git-exposure` vs `source-metadata`).

## Summary
observation 3개 소유 시그니처(PS-009/012/013) in-partition 강화 + **파티션 확장 3건**(PS-005/006 위임→native 승격 13→15, PII 신규 26-밖 카테고리, .DS_Store PS-010 편입). nuclei-templates 증류(pinned)로 손수정규식 금지·FR-6 소유-only·boot-totality·26 items 목록 불변 유지. 검증은 FP/FN 코퍼스 + PBT FULL + 통합 재확인.
