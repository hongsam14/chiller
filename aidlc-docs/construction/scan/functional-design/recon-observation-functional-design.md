# Functional Design — recon 노하우 → observation 반영 (r21)

**Unit**: scan / observation. **Grounding**: 실코드 정독 — `signatures.go`(13 시그니처)·`signatures_config.go`·`wellknown.go`·`engine.go`(sigChecker=1 시그니처 1 CWE)·`tools/sigimport/main.go`(증류 생성기)·`configs/{coverage,mappings}/kisa.yaml`·`internal/findings/coverage/coverage.go`(boot totality).

## 파티션 커널 불변식 (coverage.go New(), 설계 제약)
- **INV-1**: owned CWE는 `mt.KnownCWE(c)` 또는 `NoCanonicalCWE` (line 48).
- **INV-2**: owned item ∈ spec.Items (line 67).
- **INV-3**: item은 owned XOR delegated — **동시 금지**(line 70). → PS-005/006를 owned로 만들면 delegations에서 **반드시 제거**.
- **INV-4**: 모든 spec.Item은 owned OR delegated (line 77, totality).
- **INV-5**: `Project(CWE, KISA)=[]`면 그 CWE는 어떤 item도 소유하지 않음 → 회계 0(하지만 KnownCWE는 통과). PII의 26-밖 처리 근거.

---

## FD-0 — CWE 확정 (C-5 핀, requirements Open decision #1·#2 해소)

| PS | 항목 | Owning CWE | 근거 |
|---|---|---|---|
| PS-005 | 오류 페이지 정보 노출 | **CWE-497** (Exposure of Sensitive System Information) | 오류 응답에 내부 경로/구성/시스템 정보 노출. PS-006(스택트레이스)와 **구분**되는 고유 CWE(INV-3 대비 one-CWE-one-owner). CWE-200(부모)보다 구체적. |
| PS-006 | 스택 트레이스 노출 | **CWE-209** (Generation of Error Message Containing Sensitive Information) | 스택트레이스=CWE-209의 교과서적 사례. |
| PII | 개인정보 노출 (26-밖) | **CWE-359** (Exposure of Private Personal Information) | 주민번호=국가 ID도 CWE-359. `{KISA: []}` 빈 투영 → INV-5로 26 회계 불변. |
| .DS_Store | (PS-010 편입) | **CWE-527** (기존, 불변) | gitExposureSig 내부 확장 — 신규 CWE 없음. |
| 백업 매직 | (PS-012 강화) | **CWE-530** (기존, 불변) | backupFileSig 내부 강화. |

*비고*: PS-005=CWE-497 / PS-006=CWE-209는 상호 구분·정보노출 계열 canonical. 사용자 override 가능(FD 결정, 비구속 아님 — 확정치). 두 CWE 모두 `mappings/kisa.yaml`에 KISA 투영과 함께 등재해 INV-1 충족.

---

## FD-1 — PS-012 백업 파일 본문 매직 검증 (FR-1)
`backupFileSig.Judge` 강화. 신규 헬퍼 `looksLikeArchive(body []byte) (kind string, ok bool)` — 선두 바이트 매직:
- zip `PK\x03\x04` / `PK\x05\x06`(empty) → "zip"
- SQLite `SQLite format 3\x00` → "sqlite"
- gzip `\x1f\x8b` → "gzip"
- SQL 덤프: 선두 텍스트가 `-- MySQL dump` / `-- PostgreSQL database dump` / `-- MariaDB dump` (대소문자 무시, 선행 공백 허용) → "sqldump"
- 7z `7z\xbc\xaf\x27\x1c` / rar `Rar!\x1a\x07` → "7z"/"rar"

판정 로직(소유 CWE-530 불변):
```
name := backupName.MatchString(path)
kind, magic := looksLikeArchive(body)
if magic { fire High (매직 확인 — 이름 약해도 진짜 아카이브, FN 축소) }
else if name && !looksLikeHTML(resp) { fire High (기존 이름+not-HTML) }
else { N/A / clean }
```
- 매직 발화 시 evidence: `bodyItemAt(body, 0, len(magicPrefix), 0, ruleID(cweBackup,"backup-magic"), hyp)`, Snippet=매직 라벨(예: `"backup archive (zip)"`) — 원문 바이너리 덤프 금지, maxEvidenceLen 유지.
- **FP 가드**: 매직은 **선두 바이트**만 검사(본문 중간 우연 매치 배제). SQL 덤프는 선두 코멘트 라인만.
- **주의**: name 매치 없이 매직만으로도 발화 → well-known 스윕(`/backup.zip` 등)에서 소유-only 유지하되, **임의 제출 URL**이 zip을 반환하면(정상 다운로드 자산) FP 위험. → **경로/이름 가드 유지**: 매직 발화도 `backupName` OR well-known backup 경로일 때만(임의 `.zip` 자산 오탐 차단). 즉 `(name || isWellKnownBackupPath) && magic` → High; `name && !HTML` → High. 순수 매직-only 발화는 하지 않음(FP 안전).

## FD-2 — PS-013 제품 노출 마커 + well-known (FR-2, Q6=A)
`adminPageSig`에 nuclei-templates 증류 제품 마커 브랜치 추가(소유 CWE-306 불변).
- **증류**: `tools/sigimport`에 `genExposureMarkers()` 추가 — vendored `third_party/nuclei/misconfiguration/`(springboot actuator/env, phpinfo, h2-console, apache server-status/info) 템플릿의 `matchers.words` 수집 → RE2 마커 → 신규 `configs/signatures/exposures.yaml{ paths[], markers[] }`. 생성기 generic-drop 필터(distinctive) 재사용(예: 범용 `status`/`json` 배제).
- **소비**: `adminPageSig`에 `exposures ExposureRules` 필드. `Judge`에서 (status 200 && path∈exposures.paths 또는 body∈exposures.markers) → PS-013 발화. **콘텐츠 마커 필수**(FR-2c: 경로-only 발화 금지 — soft-404 방어, 기존 adminContentMarker 규율 일관).
- **well-known 추가**(wellknown.go, PS-013 소유, FR-6 소유-only 상속): `/actuator/env`, `/actuator/health`, `/phpinfo.php`, `/h2-console`, `/server-status`, `/server-info`. approved-host+rate 게이트 상속.
- **정직성**: 마커/경로는 nuclei-templates 증류(pinned) — 손수 금지. exposures.yaml도 `signatures-check` drift 0 대상.

## FD-3 — PS-009 추가 시크릿: 갭 검증 (FR-3)
- Code Gen에서 recon 패턴(Stripe/Slack/GitHub/npm/JWT/AWS) vs `configs/signatures/secrets.yaml`(gitleaks-증류) 대조. gitleaks 규칙집합은 `stripe-access-token`·`slack-*`·`github-pat`·`npm-access-token`·`jwt`·`aws-access-token` 등을 이미 포함 → **예상 NO-OP**.
- 갭 발견 시에만 gitleaks 형식으로 증류 추가(손수 금지, entropy/keyword/allow 포함). 실제 추가 여부·근거는 Code Gen 요약에 기록. 소유 CWE-615 불변.

## FD-4 — PS-005/006 observation-native 승격 (FR-4, **파티션 변경**)
- **증류**: `tools/sigimport`에 `genErrorDisclosure()` 추가 — vendored nuclei-templates 프레임워크-debug/에러 노출 템플릿(django-debug, laravel-debug/Whoops, symfony, rails, werkzeug/flask-debug, spring whitelabel error)의 `matchers.words` → RE2 마커. 신규 `configs/signatures/error-disclosure.yaml{ stackTrace[], errorInfo[] }`:
  - `stackTrace[]` → **PS-006/CWE-209**: `Traceback (most recent call last)`, Java `at [\w.$]+\([\w.]+\.java:\d+\)`, `\.py", line \d+`, `werkzeug`, `at .+\(.+:\d+:\d+\)`(node), Whoops/`stack trace`.
  - `errorInfo[]` → **PS-005/CWE-497**: 내부 경로/구성 노출 마커 — `Whitelabel Error Page`, `DEBUG = True`, `Application Trace`, 절대경로 노출(`/var/www/`, `/home/…/`, `C:\\inetpub\\`) 등 nuclei debug 템플릿 유래.
- **소비**: 신규 시그니처 2개(inline, config-소비):
  - `stackTraceSig{ rules []*regexp.Regexp }` — CWE() = CWE-209. `Judge`: `ctx.Response.Body`에서 첫 매치 → `findingAt(cweStackTrace, SevMedium, target, resp, bodyItem)`. **수동성(FR-4c)**: 제출/발견 응답 본문만 판정, 능동 트리거 없음.
  - `errorInfoSig{ rules []*regexp.Regexp }` — CWE() = CWE-497. 동일 패턴, SevMedium.
- **DiscoveredJS 미적용**: 스택트레이스/에러는 페이지 응답에서만 판정(JS 소스엔 무의미) — PS-009와 달리 JS 순회 안 함.
- **파티션 회계 갱신**:
  - `configs/coverage/kisa.yaml` delegations: **PS-005·PS-006 삭제** → `{PS-014: ASM, PS-026: Nuclei}` (INV-3 필수).
  - `configs/mappings/kisa.yaml` map 추가: `CWE-209: {KISA: [PS-006]}`, `CWE-497: {KISA: [PS-005]}`.
  - `signatures.go` CWE 상수: `cweStackTrace="CWE-209"(PS-006)`, `cweErrorInfo="CWE-497"(PS-005)`, hypotheses 2행 추가.
  - `engine.go New()` 레지스트리에 `stackTraceSig{}`·`errorInfoSig{}` 등록 → observation 13→15.
- **boot totality**: 두 신규 CWE가 mappings에 등재(INV-1) + delegations 제거로 disjoint(INV-3) + PS-005/006 owned로 totality(INV-4) 충족.

## FD-5 — PII 노출 신규 카테고리 (FR-5, **26-밖**)
- **inline 시그니처** `piiExposureSig` (envExposureSig처럼 config 아닌 inline RE2, provenance=OffenRed recon `stage2c_passive_sensitive_agent.py` 주석 명시). CWE() = **CWE-359**.
- **패턴**(RE2, 필드-키 앵커 — bare 값 매칭 금지, FR-5c):
  - 한국 주민번호: `\b\d{6}-[1-4]\d{6}\b` (형식 경계 — 뒤 첫자리 1~4, 랜덤 12자리 FP 축소). **추가 가드**: 앞뒤 숫자 경계(전화/카드번호 오탐 배제, `internal.match` RFC1918 경계 가드 방식 재사용).
  - email 필드: `(?i)"(?:email|e_?mail)"\s*:\s*"[^"@\s]{2,}@[\w.-]+\.[a-z]{2,}"`
  - phone 필드: `(?i)"(?:phone|mobile|tel|telephone)"\s*:\s*"\+?\d[\d\s().-]{7,}\d"`
  - 국적/여권 ID 필드: `(?i)"(?:ssn|national_id|passport|resident_id)"\s*:\s*"[\w-]{6,}"`
- **Judge**: `ctx.Response.Body` + `DiscoveredJS`(선택 — API 응답이 JS로 임베드될 수 있으나 1차는 페이지 본문만; JS는 Post 고려). 첫 매치 → finding.
- **evidence 마스킹(FR-5c, Open decision #2 해소)**: PII 값 원문 노출 금지 — Snippet은 **필드-키 + 마스킹 값**(예: `"email": "j***@***.com"` / 주민번호 `######-#######`). 마스킹 헬퍼 `maskPII(match)`. Location.Offset/MatchLen은 원문 위치 보존(다운스트림 재로케이트용), Snippet만 마스킹. Rule=`observation:CWE-359/pii-<kind>`, Hypothesis="personal information exposed in response".
- **회계**: `mappings/kisa.yaml`에 `CWE-359: {KISA: []}` (INV-5, subzy `no-canonical-cwe` 패턴) → boot KnownCWE 통과 + 26 회계 불변. `coverage/kisa.yaml` items·delegations 불변. `engine.go`에 등록(observation checker가 CWE-359 소유, Project=[]).
- **주의**: CWE-359는 어떤 PS item도 투영 안 하므로 리포트에서 KISA 배지 없이 표시(Project=[]) — 의도된 26-밖. finding 자체는 산출·리포트됨.

## FD-6 — .DS_Store 소스 노출 (FR-6, PS-010 편입)
- `gitExposureSig`(CWE-527) 내부 확장 — 신규 시그니처/CWE 없음(one-CWE-one-owner 유지).
- 경로 가드 확장: `gitPath` 유지 + 신규 `dsStorePath=^.*/\.DS_Store$`. `Judge`에서 둘 중 하나 매치 시 판정.
- .DS_Store 본문 매직: 선두 4바이트 `\x00\x00\x00\x01` + offset 4~7 `Bud1` 시그니처. 매직 확인 시 발화(HTML soft-404 배제 자연 성립 — 매직 필수).
- rule-id: 기존 `.git`은 `git-exposure`, .DS_Store는 `dsstore-exposure`(하위 rule 구분). Hypothesis: ".DS_Store discloses directory structure / filenames".
- well-known 추가: `/.DS_Store` (PS-010 소유). K7 디렉터리 목록은 **제외**(dirhunt/PS-004 probe 소유, 침범 금지).

## FD-7 — 증류 파이프라인 확장 (nuclei-templates, FR-2·FR-4)

기존 `genAPIDocs()`(nuclei `exposures-apis/*` → `api-docs.yaml`)와 **동일 메커니즘**을 확장한다. 핵심 원리 재확인(실코드):
- nuclei 템플릿 YAML은 `http[].payloads.paths[]`(또는 `path[]`)에 후보 경로, `http[].matchers[].words[]`에 본문/헤더 매칭어를 담는다(swagger-api.yaml 실측 확인).
- `collectAPIDoc(doc, paths, markers)`가 이미 재귀로 `/`-선두 문자열→paths, `words` 키 하위 문자열(≥3자)→markers를 수집한다. **이 워커를 재사용**한다(신규 파서 불요).
- 산출은 authoring-time 파일 — **런타임 미사용**(NFR-1). `make signatures-check`가 pinned 소스→산출 재현성(drift 0)을 강제.

### FD-7a — Vendoring (디렉터리=타깃 라우팅)
기존 `exposures-apis/`처럼 **디렉터리 단위로 출력 타깃을 고정**한다(파일별 분류 로직 없이 재현 가능). `third_party/nuclei/` 아래 신규 3 디렉터리:

| vendored 디렉터리 | 대상 nuclei-templates 경로(후보) | 생성기 함수 | 출력 | 소유 |
|---|---|---|---|---|
| `exposures-product/` | `http/misconfiguration/springboot/springboot-actuator.yaml`·`springboot-env.yaml`·`http/misconfiguration/php/phpinfo*.yaml`(또는 `exposures/configs/phpinfo-files.yaml`)·`http/misconfiguration/apache/apache-server-status*.yaml`·`apache-server-info.yaml`·`http/technologies/h2-database-detect.yaml` | `genExposureMarkers()` | `exposures.yaml` | **PS-013 / CWE-306** |
| `errors-stacktrace/` | `http/misconfiguration/django-debug-detect.yaml`·`laravel-debug-enabled.yaml`(Whoops/Ignition)·`symfony-debug*.yaml`·`flask-debug*`/werkzeug 관련·rails 관련 | `genErrorDisclosure()` → `stackTrace[]` | `error-disclosure.yaml` | **PS-006 / CWE-209** |
| `errors-info/` | `springboot-whitelabel`·debug-mode-on/verbose 설정 노출 계열 | `genErrorDisclosure()` → `errorInfo[]` | `error-disclosure.yaml` | **PS-005 / CWE-497** |

- 각 디렉터리에 `SOURCE`(repo·files 목록·commit SHA·license) — **디렉터리별 provenance**. commit은 기존 핀 `4924c84…` 유지 시도, 대상 템플릿이 그 커밋에 존재하는지 Code Gen vendoring에서 확인 후 필요 시 단일 SHA로 bump(전 nuclei vendored 동시 갱신).
- 실측 정직성 주의: 위 nuclei 경로/파일명은 **후보**다. Code Gen에서 실제 레포를 pinned SHA로 체크아웃해 (i) 존재하는 파일만 vendor, (ii) 각 파일의 실제 `words`를 산출에 반영, (iii) SOURCE `files:`에 vendored 실경로를 정확 기재. 존재하지 않는 템플릿은 **조용히 누락 금지** — 생성기 stderr에 로깅(gitleaks skipped 방식과 동일).

### FD-7b — 추출 알고리즘 (worked example)
`genExposureMarkers()` (PS-013):
1. `errors...`가 아닌 `exposures-product/` 디렉터리를 `collectAPIDoc` 스타일로 순회 → `paths{}`, `markers{}` 수집.
2. `paths` 필터 `distinctiveExposurePath`(신규): 제품 노출 경로만(예 `/actuator`,`/actuator/env`,`/phpinfo.php`,`/server-status`,`/h2-console`) — 범용 `/`,`/api` 등 배제(api-docs distinctive 필터 재사용 패턴).
3. `markers` 필터 `distinctiveExposureMarker`(신규): 제품-식별 문구만 keep(`activeProfiles`,`propertySources`,`phpinfo()`,`Apache Server Status`,`H2 Console`), 범용어 drop(`status`,`json`,`_links` 단독 등 FP 유발).
4. `exposures.yaml{ paths: []string, markers: []string }` 출력(**substring 매칭용** — api-docs BodyMarkers와 동일 소비 방식).

**예시**(springboot-env.yaml의 `words: ["activeProfiles","propertySources"]` → `exposures.yaml.markers += ["activeProfiles","propertySources"]`; `path: ["{{BaseURL}}/actuator/env"]` → `paths += ["/actuator/env"]`).

`genErrorDisclosure()` (PS-005/006):
1. `errors-stacktrace/`·`errors-info/`를 **각각** 순회 → 디렉터리별 `words` 수집.
2. 각 word를 **RE2 리터럴로 승격**: `regexp.QuoteMeta(word)` → 안전한 정규식(백트래킹 없음). `re2ok` 재검(불변식).
3. 필터 `distinctiveErrorMarker`(신규, FP 핵심): keep 조건 = (다중 단어 구 `\s`포함 && 길이≥18) OR (구조적 마커 화이트리스트에 속함). drop = 단독 범용어(`error`,`exception`,`warning`,`debug`,`trace`,`fail` 단독) — 임의 페이지 FP 차단.
4. `errors-stacktrace/` 산출 → `stackTrace[]`, `errors-info/` 산출 → `errorInfo[]`.
5. `error-disclosure.yaml{ stackTrace: []string(RE2), errorInfo: []string(RE2) }` 출력.

**예시**(django-debug-detect.yaml `words: ["You're seeing this error because you have DEBUG = True","Traceback (most recent call last)","Request Method:"]` → `QuoteMeta` → keep 앞 2개(구·길이 충족), `Request Method:` drop(범용)).

### FD-7c — recon 구조적 스택프레임 (nuclei가 words로 안 담는 것)
nuclei `words`는 리터럴 문구 위주라 **언어-런타임 스택 프레임 구조**(Java `at pkg.Cls(File.java:42)`, Node `at fn (/app/x.js:10:5)`, PHP `#0 /var/www/…`)를 거의 안 담는다. 이는 recon(`stage2c_signal_patterns.py`/`response_signals.py`)의 검증된 구조 정규식에서 **증류**한다:
- **provenance 소스**: `third_party/offenred-recon/` 신규 디렉터리 — `SOURCE`(repo=로컬 형제 `OffenRed-AI-DAST`, files=해당 recon 파일, commit=그 레포 git SHA) + `stacktrace-frames.txt`(큐레이트된 RE2 구조 정규식, 1행 1패턴, 주석 provenance). ~6패턴: Java `\bat [\w.$]+\([\w ]+\.java:\d+\)`, Python `File "[^"]+\.py", line \d+`, Node `\bat .+\(.+:\d+:\d+\)`, PHP `#\d+ /\S+\.php\(\d+\)`, Ruby `\.rb:\d+:in `, Werkzeug/`Traceback (most recent call last)`.
- 생성기 `genErrorDisclosure()`가 이 txt를 읽어 각 행 `re2ok` 검증 후 `stackTrace[]`에 **union**(nuclei 리터럴 + recon 구조). 둘 다 provenance 배너에 명시(`from nuclei-templates … + OffenRed recon …`).
- 이로써 "손수 정규식 금지"는 유지된다 — 정규식은 검증된 OSS/recon 소스에서 authoring-time 증류, 임의 창작 아님([[gitleaks-secret-patterns]] 규율과 동일 정신).

### FD-7d — 생성기 배선 & 재현성
- `run()`에 `genExposureMarkers()`·`genErrorDisclosure()` 추가. 실패 시 fail(기존 패턴).
- `writeYAML` 배너에 provenance(디렉터리별 SOURCE + skipped 카운트). 재실행 멱등 → `make signatures-check` drift 0.
- **신규 Go dep 0**(yaml.v3/toml만). **런타임 미사용** — 산출 yaml만 커밋, `third_party/` 원본·txt·생성기는 authoring-time.
- `.gitleaks.toml`: 신규 코퍼스/vendored 파일이 시크릿-형 리터럴을 담으면 정확-리터럴 allowlist 등재(NFR-1, full-history secret-scan green 유지).

## FD-8 — signatures_config.go 로더 확장
`Rules` 구조체 신규 필드 + 대응 yaml 로더:
- `Exposures ExposureRules{ Paths []string; Markers []string }` ← `exposures.yaml`(substring 소비, api-docs 로더와 동형).
- `StackTrace []*regexp.Regexp`·`ErrorInfo []*regexp.Regexp` ← `error-disclosure.yaml`의 `stackTrace[]`/`errorInfo[]`(로드 시 `regexp.Compile`, RE2).
- fail-closed(기존 규율): yaml 파싱 실패 / 빈 규칙집합 / 정규식 컴파일 실패 → **로드 에러**(런타임 시그니처 조용한 무력화 금지). `signatures_config_test.go`에 fail-closed 회귀 추가.
- `New(Rules)`가 `adminPageSig{exposures:…}`·`stackTraceSig{rules:StackTrace}`·`errorInfoSig{rules:ErrorInfo}`에 주입. `Default()`(빈 규칙)는 테스트용 유지 — 단, 신규 시그니처는 빈 규칙이면 **항상 clean 반환**(nil 규칙 안전, PD-B식 nil-safe).

## FD-9 — 검증 (FR-7, PBT FULL)
- **FP/FN 코퍼스**(각 신규/강화 시그니처):
  - PS-012: 진짜 zip/SQLite/SQL덤프 매직 양성 · report.zip(정상자산 아님, 이름無)·soft-404 HTML 음성.
  - PS-013 exposures: actuator env JSON/phpinfo 양성 · 홈페이지 soft-404 음성.
  - PS-005/006: Django/Laravel debug·Java 스택트레이스 양성 · 정상 200·버전문자열 음성.
  - PII: 실 주민번호/email 필드 양성 · 랜덤 12자리·전화번호·`userType` 음성.
  - .DS_Store: 실 Bud1 매직 양성 · .DS_Store 이름인데 HTML 음성.
- **PBT FULL**: (a) 경계 — Location.Offset/MatchLen이 원문 인덱스, PII 마스킹 후에도 offset 불변; (b) 소유-only — 신규 well-known(actuator/phpinfo/.DS_Store) 스윕이 소유 시그니처만 실행(FR-6); (c) 매직 판별 항등 — 매직 present ⟺ looksLikeArchive; (d) **파티션 불변** — boot totality 통과, 26 items·disjoint(PS-005/006 owned XOR delegated), CWE-359 Project=[]; (e) PII 마스킹 — 원문 값이 Snippet에 안 실림.
- **통합 재확인**: end-to-end orchestrator 스캔에서 신규 CWE(497/209/359/530-magic/527-dsstore) finding 산출 + 26 커버리지 회계 정합. probe(9)·소유권·신규dep0 불변.

## §자기검증 대상 (다음 단계 전 점검 예정)
- PS-005/006 CWE 상호구분(497 vs 209) 적정성 · PII CWE-359 {KISA:[]} 회계 무영향 재확인 · 백업 매직-only 발화 FP 가드(경로/이름 필수) · nuclei-templates 실제 stacktrace 템플릿 존재성(Code Gen vendoring 시 확인) · .DS_Store를 git 시그니처에 편입 시 rule-id 정직성.

## Open → Code Generation
1. nuclei-templates pinned SHA + 정확한 vendored 템플릿 파일 목록(FD-7).
2. FR-3 gitleaks 갭 실제 대조 결과(예상 NO-OP).
3. exposures/error-disclosure yaml의 최종 마커 셋(증류 산출).
