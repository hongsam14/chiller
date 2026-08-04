# Code Generation Plan — recon 노하우 → observation (r21)

**Unit**: scan/observation. **Single source of truth for Code Generation.**
**Strategy**: 증류 원본 먼저(vendoring→생성기→config) → domain CWE → 파티션 회계 → 시그니처 → 레지스트리/well-known → 검증. 각 스텝 부분빌드 수용기준.

## Context / Dependencies
- **FD**: `recon-observation-functional-design.md` (FD-0..9). **Requirements**: `requirements-recon-observation.md`.
- **불변**: OSS-증류(손수 금지)·checker↔CWE 소유권·FR-6 소유-only·26 items·boot-totality(coverage.go INV-1..5)·RE2·신규 Go dep 0.
- **외부 의존**: nuclei-templates 클론(GitHub 접근 확인됨, HEAD `9419e84…`) — **pinned SHA로 vendoring**. OffenRed recon(로컬 형제 레포) 스택프레임 증류.
- **파일 소재**: 앱 코드=workspace root(`internal/`, `configs/`, `third_party/`, `tools/`), 문서=`aidlc-docs/construction/scan/code/`.

---

## Step A — 증류 원본 vendoring (pinned) + provenance
**(자기검증 PD-B)** provenance()는 `third_party/<src>/SOURCE`를 읽으므로 `provenance("nuclei/errors-stacktrace")`처럼 **서브디렉터리별 SOURCE**가 그대로 동작 → 단일 SHA로 전 nuclei 동기화 bump 불필요. 기존 `exposures-apis/` + 그 `third_party/nuclei/SOURCE`(api-docs.yaml 유래)는 **손대지 않음**(api-docs.yaml drift 0 유지). 신규 서브디렉터리는 각자 SOURCE·독립 핀.
- [x] A1. nuclei-templates를 선택 pinned SHA로 임시 클론(신규 서브디렉터리용; 기존 api-docs 핀과 독립). 대상 템플릿 실존 확인.
- [x] A2. `third_party/nuclei/exposures-product/`에 실존 템플릿만 vendor: springboot-actuator·springboot-env·phpinfo·apache-server-status·apache-server-info·h2-database-detect(실경로 확인). 서브디렉터리 `SOURCE`(repo/files 실경로/commit/license MIT) + `LICENSE`.
- [x] A3. `third_party/nuclei/errors-stacktrace/`에 vendor: django-debug-detect·laravel-debug(Whoops/Ignition)·symfony-debug·werkzeug/flask-debug·rails(실존 확인). 서브디렉터리 `SOURCE`+`LICENSE`.
- [x] A4. `third_party/nuclei/errors-info/`에 vendor: springboot-whitelabel·debug-mode/verbose 계열(실존 확인). 서브디렉터리 `SOURCE`+`LICENSE`.
- [x] A5. **(자기검증 PD-D)** `third_party/offenred-recon/`: `SOURCE`(repo=OffenRed-AI-DAST 로컬 형제(enki 내부·동일 조직), files=stage2c_signal_patterns.py/response_signals.py, commit=그 레포 git SHA) + `stacktrace-frames.txt`(큐레이트 RE2 6패턴, 1행1패턴+주석 provenance). **LICENSE 파일 불요**(내부 동일조직): 복사하는 것은 스택프레임 **형식 사실**(Java `at …(File.java:NN)` 등 공개 규약, nuclei/OWASP와 교차확인 가능)뿐이며 proprietary 코드 원문 미복사 — provenance 주석으로 충분. SOURCE에 "internal, format-facts only" 명시.
- [x] A6. 부재 템플릿은 **조용히 누락 금지** — vendored 실목록을 SOURCE에 정확 기재, 계획 대비 차이를 code 요약에 로깅.
- **수용**: `third_party/` 신규 서브디렉터리 + SOURCE(+nuclei LICENSE) 존재, 실경로 정확, 기존 api-docs vendored/핀 불변. 클론 임시본 삭제(원본만 커밋).

## Step B — 생성기 확장 (tools/sigimport)
- [x] B1. `genExposureMarkers()` — `exposures-product/` 순회(collectAPIDoc 재사용) → `distinctiveExposurePath`/`distinctiveExposureMarker` 필터 → `exposures.yaml{paths[],markers[]}`(substring). provenance 배너.
- [x] B2. `genErrorDisclosure()` — `errors-stacktrace/`·`errors-info/` 각 순회 → `words` QuoteMeta→RE2 + `offenred-recon/stacktrace-frames.txt` union(stackTrace) → `distinctiveErrorMarker` 필터 → `error-disclosure.yaml{stackTrace[],errorInfo[]}`. re2ok 재검. provenance 배너(nuclei+recon).
- [x] B3. `run()`에 두 함수 배선. 부재/스킵 stderr 로깅(기존 skipped 패턴).
- [x] B4. `make signatures` 실행 → `configs/signatures/exposures.yaml`·`error-disclosure.yaml` 생성. `make signatures-check` drift 0 확인.
- **수용**: 생성기 빌드/실행 green, 신규 yaml 산출, 재실행 멱등(drift 0), 신규 Go dep 0.

## Step C — domain CWE 상수 + hypotheses
- [x] C1. `signatures.go` CWE 상수 추가: `cweStackTrace="CWE-209"`(PS-006)·`cweErrorInfo="CWE-497"`(PS-005)·`cwePII="CWE-359"`. (.DS_Store·backup는 기존 CWE-527/530 재사용.)
- [x] C2. `hypotheses` 맵 3행 추가(stacktrace/errorinfo/pii). backup-magic·dsstore는 기존 hyp 재사용 또는 sub-rule.
- **수용**: 컴파일 green.

## Step D — 파티션 회계 config (coverage + mappings)
- [x] D1. `configs/coverage/kisa.yaml` delegations에서 **PS-005·PS-006 삭제** → `{PS-014: ASM, PS-026: Nuclei}`. items 26 목록 불변.
- [x] D2. `configs/mappings/kisa.yaml` map 추가: `CWE-209: {KISA:[PS-006]}`·`CWE-497: {KISA:[PS-005]}`·`CWE-359: {KISA:[]}`. **(자기검증 PD-C)** 헤더 주석 line 9 완전 갱신: "22 covered items (10 observation + 12 probe)" → **"24 covered items (15 observation + 9 probe)"**, delegated 목록 "PS-005/006/014/026" → **"PS-014/026"**. (현 breakdown "(10 obs+12 probe)"는 #0d 쿠키 이동 후 이미 낡음 — 정정.)
- **수용**: yaml 유효. (boot-totality는 Step G 등록 후 성립 — 이 시점 단독 로드 OK.)

## Step E — 시그니처 강화/신규 (signatures.go)
- [x] E1. **FR-1 백업 매직**: `looksLikeArchive(body) (kind,ok)` 헬퍼(zip/sqlite/gzip/sqldump/7z/rar 선두 매직). **(자기검증 PD-A)** 매직은 **FP 축소 확증**이지 FN 트리거가 아님(매직-only 발화는 정상 `.zip` 다운로드 자산을 오탐 → 금지). `backupFileSig.Judge` 강화: `name && magic` → High(**진짜 아카이브 확증** — not-HTML 휴리스틱의 오탐 제거); `name && !HTML && !magic` → High(레거시 호환 유지); 매직/이름 둘 다 없으면 clean. `name`=`backupName`(well-known backup 경로도 이미 backup-named이라 포함). evidence Snippet=매직 라벨. requirements FR-1b "FN 축소" 표현은 "FP 축소"로 정정.
- [x] E2. **FR-6 .DS_Store**: `gitExposureSig` 확장 — `dsStorePath` 가드 + Bud1 매직(`\x00\x00\x00\x01`+offset4 `Bud1`). rule-id `dsstore-exposure`. CWE-527 불변(신규 시그니처 아님).
- [x] E3. **FR-4 스택트레이스**: `stackTraceSig{rules []*regexp.Regexp}` — CWE()=cweStackTrace. Judge: Response.Body 첫 매치→findingAt(SevMedium). nil 규칙 안전(clean).
- [x] E4. **FR-4 에러정보**: `errorInfoSig{rules []*regexp.Regexp}` — CWE()=cweErrorInfo. 동일 구조.
- [x] E5. **FR-2 제품노출**: `adminPageSig`에 `exposures ExposureRules{Paths,Markers}` 필드 + Judge에 exposures 브랜치(status 200 && (path∈paths OR body contains marker)→PS-013 발화, 콘텐츠마커 필수). CWE-306 불변.
- [x] E6. **FR-5 PII**: `piiExposureSig{}` — CWE()=cwePII. inline RE2(주민번호 경계가드+email/phone/national-id 필드-키 앵커, provenance=recon 주석). `maskPII(match)` 값마스킹(Snippet만 마스킹, Location.Offset/MatchLen 원문 보존). Judge: Response.Body 첫 매치→findingAt. **(자기검증 PD-F)** 심각도 핀: 주민번호/national-id=**SevHigh**(민감 식별자), email/phone=**SevMedium**. 매치 kind별 severity 결정.
- **수용**: 컴파일 green, 각 시그니처 Signature 인터페이스 충족.

## Step F — 로더 + 레지스트리 + well-known
- [x] F1. `signatures_config.go` `Rules`에 `Exposures ExposureRules`·`StackTrace/ErrorInfo []*regexp.Regexp` + yaml 로더(fail-closed: 파싱실패/빈규칙/컴파일실패→에러). `signatures_config_test.go` fail-closed 회귀.
- [x] F2. `engine.go New(rules)` 레지스트리에 `stackTraceSig{rules:…}`·`errorInfoSig{rules:…}`·`piiExposureSig{}` 추가 + `adminPageSig{exposures:…}` 주입. observation 13→15(+PII 26-밖). `Default()` nil-safe 유지. **(자기검증 PD-E)** 신규 시그니처의 CWE가 `registry.OwnershipForFindings()`→`coverage.New` 경로로 흘러 boot-totality에 반영되는지 확인(sigChecker.OwnedCWEs 자동 — New() 산출이 registry에 소비되는 경로 검증).
- [x] F3. `wellknown.go` `WellKnownPaths()` 추가: `/actuator/env`·`/actuator/health`·`/phpinfo.php`·`/h2-console`·`/server-status`·`/server-info`(PS-013 cweAdmin) + `/.DS_Store`(PS-010 cweGit). FR-6 소유-only 상속.
- **수용**: 컴파일 green, 오케스트레이터 부팅 시 boot-totality 통과(신규 CWE 매핑됨, PS-005/006 owned XOR delegated).

## Step G — FR-3 gitleaks 갭 대조
- [x] G1. `configs/signatures/secrets.yaml`(gitleaks-증류)에서 stripe/slack/github/npm/jwt/aws 규칙 존재 확인. 존재하면 **NO-OP**(근거를 code 요약에 기록).
- [x] G2. 갭 발견 시에만 gitleaks 규칙 형식으로 증류 추가(손수 금지). 예상: NO-OP.
- **수용**: 대조 결과 문서화. secrets.yaml 변경 시 signatures-check drift 0.

## Step H — 테스트 (FP/FN 코퍼스 + PBT FULL)
- [x] H1. FP/FN 코퍼스(신규 `*_test.go`): 백업매직(zip/sqlite/sqldump 양성·report.zip/soft-404 음성)·exposures(actuator env/phpinfo 양성·홈페이지 음성)·stacktrace/errorinfo(django/laravel/java 양성·정상200/버전문자열 음성)·PII(실주민번호/email 양성·랜덤12자리/전화/userType 음성)·.DS_Store(Bud1 양성·이름만HTML 음성).
- [x] H2. PBT FULL: (a)경계 offset/len 원문+PII마스킹후 offset불변 (b)소유-only 신규 well-known 스윕 (c)매직판별 항등 (d)파티션불변 boot-totality/disjoint/CWE-359 Project=[] (e)PII 원문값 미노출.
- [x] H3. 파티션 회계 테스트: coverage boot에서 PS-005/006 owned·PS-014/026 delegated·CWE-359 26-미포함 단언. **(자기검증 PD-E)** CWE-359 finding이 `Account()`(spec.Items만 순회 → 무영향)·report view(ItemCWEs에 없음 → KISA 배지 없이 finding만 표시)를 **에러 없이** 통과하는지 명시 단언(Q4=B 핵심 안전성, 실코드 확증: Account는 spec.Items만 iterate).
- **수용**: `go test ./... -race` green.

## Step I — 통합 + 전 게이트 + 요약
- [x] I1. end-to-end orchestrator 스캔에서 신규 CWE(497/209/359, backup-magic 530, dsstore 527) finding 산출 + 26 커버리지 회계 정합 확인(기존 e2e 테스트 확장).
- [x] I2. 전 게이트: build/vet/gofmt/`make arch-check`/`go test -race`/`make gosec`(0)/`make secrets`(gitleaks allowlist 갱신 후 no-leak)/`make signatures-check`(drift 0).
- [x] I3. `aidlc-docs/construction/scan/code/recon-observation-summary.md` 작성(변경 파일·불변 검증·vendored 실목록·gitleaks 갭결과·CWE 확정). README Status/Roadmap 갱신(PS-005/006 native·PII·제품노출).
- **수용**: 전 게이트 green, 요약 작성, 불변 재확인.

---

## 자기검증 (근거추출→요구사항/디자인 필터→플랜 리뷰) — 완료

**근거 추출 → 필터(요구사항/FD/실코드 대조):**
- P-R1(collectAPIDoc 재사용 가능) — ✅ 유효(실코드: `/`-paths + `words` 재귀 수집).
- P-R3(PS-005/006 native면 delegations 제거 필수, INV-3 disjoint) — ✅ 유효(coverage.go:70 owned∩delegated 금지).
- **P-R4(CWE-359 {KISA:[]}는 boot 통과+회계 0)** — ✅ **실코드 확증**: `Account()`가 `spec.Items`만 순회 → CWE-359는 어떤 PS-ID에도 안 실려 무영향. `KnownCWE(359)`=true, `Project(359)=[]`.
- P-R9(piiExposureSig 등록→ownership 자동) — ✅ 유효(registry.OwnershipForFindings→OwnedCWEs), 단 registry 소비 경로 확인 필요(→PD-E).
- P-R2(mappings "22 covered" 갱신) — ⚠️ **불완전**: "(10 obs+12 probe)" breakdown도 낡음(#0d 후 13+9) → 완전 갱신(PD-C).
- P-R5/6(매직=FN 축소, `(name||wellknown)&&magic`) — ⚠️ **오류**: 매직-only 발화는 정상 .zip 자산 FP → 금지. 매직은 **FP 축소 확증**. well-known backup은 이미 backup-named이라 `||` 중복 → `name&&magic`(PD-A).
- P-R7(단일 SHA로 전 nuclei 동기화 bump) — ⚠️ **과다명세**: provenance()가 서브디렉터리 SOURCE 지원 → 독립 핀, api-docs 불변(PD-B).
- P-R8(offenred-recon LICENSE 파일) — ⚠️ **부적절**: 내부 동일조직·형식 사실만 복사 → provenance 주석 충분, LICENSE 불요(PD-D).
- P-R10(PII severity 미핀) — ⚠️ 핀 필요(PD-F: RRN/national-id=High, contact=Medium).

**유효+정정 근거 기반 플랜 정정(PD-A..F 반영 완료):** E1(매직 FP-축소 재정의)·A1/A5(독립 핀·LICENSE 불요)·D2(주석 완전 갱신)·F2/H3(CWE-359 finding 무영향 명시 단언)·E6(severity 핀). 전부 위 스텝에 반영.
**긍정 확인**: P-R4가 Q4=B(PII 26-밖) 안전성의 핵심인데 실코드(`Account` spec.Items-only)로 확증 → 리스크 해소.

## 요약
9 스텝(A vendoring → B 생성기 → C domain → D 회계 → E 시그니처 → F 로더/레지스트리/well-known → G 갭대조 → H 테스트 → I 통합/게이트). 불변 전부 유지, 신규 Go dep 0, authoring-time 증류 재현성(signatures-check drift 0).
