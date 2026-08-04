# Code Generation Summary — recon 노하우 → observation (r21)

**Branch**: `feat/recon-observation`. **Status**: CODE COMPLETE + 전 로컬 게이트 GREEN.

## 무엇을 했나
OffenRed-AI-DAST passive/recon 노하우를 observation 엔진에 흡수. 3개 소유 시그니처 강화(in-partition) + 파티션 확장 3건. 전 불변 유지.

## 파일 변경
**신규 증류 원본 (authoring-time, pinned)**
- `third_party/nuclei/exposures-product/` — springboot-actuator·springboot-env·phpinfo-files·apache-server-status (nuclei @ `9419e84`, MIT) → `exposures.yaml`
- `third_party/nuclei/errors-stacktrace/` — laravel-debug-error·flask-werkzeug-debug·werkzeug-debugger-detect·rails-debug-mode → `error-disclosure.yaml` stackTrace[]
- `third_party/nuclei/errors-info/` — django-debug-config-enabled·laravel-debug-enabled·symfony-debug·laravel-debug-infoleak → errorInfo[]
- `third_party/offenred-recon/stacktrace-frames.txt` — 6 큐레이트 RE2 런타임 프레임(Java/Python/Node/PHP/Ruby/traceback), provenance-only(내부 동일조직, 형식 사실만)

**생성기** `tools/sigimport/main.go` — `genExposureMarkers()`·`genErrorDisclosure()` 추가(collectDir/collectMarkers: words + dsl contains-literals 추출, QuoteMeta→RE2, distinctive 필터 + all-digit drop, recon 프레임 union). 신규 Go dep 0.

**생성 config** `configs/signatures/exposures.yaml`(paths 17, markers 12)·`error-disclosure.yaml`(stackTrace 9, errorInfo 10). `make signatures-check` drift 0.

**파티션 회계**
- `configs/coverage/kisa.yaml` — delegations에서 **PS-005·PS-006 제거**(4→2: PS-014 ASM, PS-026 Nuclei).
- `configs/mappings/kisa.yaml` — `CWE-209→PS-006`·`CWE-497→PS-005`·`CWE-359→{KISA:[]}` 추가, 헤더 주석 갱신(24 covered / 15 observation + 9 probe).
- `configs/mappings/fsec.yaml` — CWE-209/497/359 빈 FSEC 투영(boot totality; FSEC 항목 아님).

**시그니처** `internal/scan/checking/observation/signatures.go`
- PS-012 `backupFileSig` — `looksLikeArchive`(zip/sqlite/gzip/7z/rar/SQL덤프 선두 매직). **매직=FP 축소 확증**(name 게이트 유지, 매직-only 발화 금지); `name&&magic`→High, `name&&!HTML`→레거시 High.
- PS-010 `gitExposureSig` — `.DS_Store` 편입(dsStorePath + Bud1 매직, CWE-527 단일 소유 유지, rule `dsstore-exposure`).
- PS-013 `adminPageSig` — `exposures ExposureRules` 브랜치(제품 마커 확인 필수, soft-404 방어).
- PS-006 `stackTraceSig`(CWE-209)·PS-005 `errorInfoSig`(CWE-497) — config-소비 inline, 응답 본문만 판정(수동성), nil-safe.
- PII `piiExposureSig`(CWE-359) — 주민번호 digit-경계 가드·email/phone/national-id 필드-키 앵커·`maskPII` 값 마스킹(offset 원문 보존). severity: RRN/national-id=High, contact=Medium.

**로더/레지스트리/well-known**
- `signatures_config.go` — `Exposures`·`StackTrace`·`ErrorInfo` 필드 + `ExposureRules` + fail-closed 로더(빈 규칙/컴파일 실패→에러).
- `engine.go New()` — 신규 3 시그니처 등록(observation 13→15 + PII 26-외 = 16 registered).
- `wellknown.go` — `/actuator/env`·`/actuator/health`·`/phpinfo.php`·`/server-status`·`/server-info`(PS-013)·`/.DS_Store`(PS-010) 추가, FR-6 소유-only 상속.

**테스트**
- `recon_observation_test.go` — FP/FN 코퍼스(backup magic·.DS_Store·exposures·stacktrace·errorinfo·PII) + PBT(PII 값 미노출·offset 원문 재로케이트·offset 경계).
- 파급 수정: 시그니처 카운트 16, registry ownership 23, coverage-wiring PS-005/006 Checked·PS-026 delegated, e2e 회계 15 observation/2 delegated + submit rate 상향(신규 well-known 볼륨), fail-closed 픽스처에 신규 yaml.

## FR-3 (추가 시크릿) — NO-OP 확인
gitleaks-증류 `secrets.yaml`(221 rules)이 stripe-access-token·slack-*·github-*·npm-access-token·jwt·aws-access-token/bedrock을 **이미 포함** → 추가 없음(손수 정규식 금지 유지).

## 정직한 이월/편차
- **H2 콘솔·범용 apache server-info**: pin `9419e84`에 대응 nuclei 템플릿 부재 → 증류 제외(SOURCE note 기재). `/h2-console` well-known 미추가(마커 없이는 발화 불가).
- nuclei-templates 실 vendoring으로 "후보 경로" FD 리스크 해소 — 실존 파일만 vendor, SOURCE에 실경로 정확 기재.

## 코드 리뷰 (workflow high, 20 agents) + 수정
10건 확정(17→10 dedup, 1 refuted). **8 수정 + 2 의도적 trade** + 회귀 테스트:
- **F1 🔴 maskPII 값 누출**(correctness): `LastIndex(":")`가 email 값 내부 콜론에서 분할→로컬부 일부 노출. **수정**: 키 닫는 따옴표 기준으로 값 전체 마스킹. 회귀: 콜론-포함 값 PBT.
- **F2 🟠 resident-id 오탐**(correctness): 무앵커 `\d{6}-[1-4]\d{6}`가 `202401-1000042`(송장번호) HIGH 오탐. **수정**: YYMMDD 생일 유효성(월01-12·일01-31)+경계가드. 회귀: bad-month 음성.
- **F3 🟠 email/phone 미탐**(correctness): 정확 키만 매칭→`userEmail`/`mobilePhone` 미탐. **수정**: 키 `[\w]*` 접두(email은 접미도) 확장(값 앵커가 오탐 방어; phone은 bare `tel` 제거). 회귀: compound key 양성 + telemetry 음성.
- **F4 🟠 symfony 헤더 마커**(correctness): `x-debug-token-link:`=헤더 마커인데 본문만 스캔→미발화 죽은 규칙. **수정**: 생성기가 `part: header` 마커 스킵(exposures의 content-type 마커도 제거). symfony 본문 마커는 유지.
- **F5 ⚪ /env 접미 오탐**(correctness): `HasSuffix("/env")`가 `/api/config/env` 오탐. **수정**: `distinctiveExposurePath`에서 bare `/env` 제외(`/actuator/env`는 유지).
- **F8 ⚪ /actuator/health 죽은 probe**(cleanup): 마커 없어 발화 불가한 well-known. **수정**: 제거.
- **F9 ⚪ FindAllIndex 낭비**(efficiency): 첫-매치 kind에도 전체 매치 할당. **수정**: 비-resident-id는 `FindIndex`.
- **F10 ⚪ 복붙 중복**(simplification): stackTraceSig/errorInfoSig 동일 구조. **수정**: `bodyMarkerSig{cwe,sub,rules}` 단일 타입(distinct 인스턴스로 one-CWE-one-owner 유지).
- **F6 skipped (의도적 trade)**: stackTrace 게이트 없음→코드-전시 페이지 오탐 가능. 수동 스택트레이스 탐지의 본질적 한계이며 마커는 nuclei 방식 그대로 OSS-증류 — 코퍼스 없이 과도 조이면 미탐 유발. 문서화된 한계로 수용.
- **F7 no_change_needed (의도)**: PS-005/006 위임→수동 승격은 사용자 결정(Q3=B). 능동 트리거 상실은 의도된 trade(수동 엔진). coverage Checked 표기는 설계대로.

수정 후 전 게이트 재검 green(build/vet/gofmt/arch-check/-race/lint 0/gosec 0/secrets no-leak/signatures-check 멱등). 26-파티션·소유권·boot-totality 불변 유지.

## 불변 검증
- **26-파티션 items 목록 불변**(coverage items 26). **소유권 one-CWE-one-owner**(.DS_Store=CWE-527, 백업매직=CWE-530 기존 소유 내 강화). **boot totality** 통과(KISA+FSEC, 신규 CWE 매핑됨, PS-005/006 owned XOR delegated). **CWE-359 회계 0**(Account가 spec.Items만 순회, Project=[]). **FR-6 소유-only** 신규 well-known 상속. **RE2**·**신규 Go dep 0**·**probe(9) 불변**.
- 게이트: build·vet·gofmt·arch-check·`go test -race`(0)·lint(0)·gosec pinned(0)·secrets(no leaks)·signatures-check(drift 0).
