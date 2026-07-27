# Functional Design — Observation 오탐 저감 (r16)

경량 FD. 요구사항 FR-1~10의 구조 결정만 확정한다(정규식 문자열은 Code Gen에서 OSS 증류).

## D1. FR-6 — well-known 소유-시그니처-only (증폭A 제거)

**현재**: `WellKnownPaths() []string`(bare 경로) · `WorkItem.WellKnown bool` · exec가 well-known
항목에 `observationOnly()`(관측 13종 전부) 실행 · fold `ranCheckers`가 `observationCheckers()`로 미러.

**변경**:
1. `observation.WellKnownPaths()` → `[]WellKnownPath{ Path string; CWE domain.CWE }`. 각 경로에
   소유 시그니처 CWE 부여: robots→CWE-651 · `.env`→CWE-538 · `.git/*`→CWE-527 · crossdomain→CWE-942
   · backup(zip/sql/bak…)→CWE-530 · admin(*)→CWE-306.
2. `domain.WorkItem`에 `WellKnownCWE domain.CWE bson/json ",omitempty"` 추가(설정 시 well-known,
   기존 `WellKnown bool`은 `WellKnownCWE != ""`로 대체 가능하나 하위호환 위해 둘 다 유지·동기).
3. `orchestrator/fanout.wellKnownItems`: 각 항목에 `WellKnownCWE = path.CWE` 세팅.
4. exec: well-known 항목이면 `checkers = observationForCWE(e.urlScoped, wi.WellKnownCWE)` — id가
   `"observation:"+CWE`인 **단일** 시그니처만. (기존 `observationOnly` 대체.)
5. fold `ranCheckers`: 동일 미러 — well-known 항목은 그 단일 소유 시그니처만 반환.

**불변**: 비소유 시그니처(PS-009/003/023/쿠키/plaintext)는 **제출 URL 항목**에서 계속 실행 →
커버리지 무영향. 소유 시그니처는 제출+well-known 양쪽 → 26 파티션·소유권 불변. exec와 fold의
미러 규율(정직성)은 r10 그대로 유지(둘이 어긋나면 skipped 시그니처를 Checked로 오기록).

## D2. FR-7 — **이월** (dedup 계약 보존)

코드 규약 리뷰(2026-07-28) 결과 **이월**. 이유: `internal/findings/dedup`는 문서상
"`IdentityKey`(R2)만 사용, 자체 identity 로직 미파생" + "P2 멱등(dedup∘dedup=dedup)"을 보장하는데,
당초 계획한 evidence-기반 2차 병합은 이 계약을 위반한다. FR-6가 관측된 증폭(well-known 13×)을
이미 제거하므로, 잔여(여러 제출 URL이 JS 공유)를 위해 dedup 계약을 깨는 건 과하다. 이월(Backlog):
계약 유지하며 exec/discovery 계층에서 DiscoveredJS 자산 중복제거(항목-간 상태공유, 큰 변경).

## D3. 증류 시그니처 = **config 파일로 외부화** (data, not code)

코드 규약 리뷰 결과, 증류한 룰 **데이터**는 Go에 하드코딩하지 않고 **config 파일로 외부화**한다 —
레포가 이미 `configs/mappings|coverage/*.yaml`을 부팅 로드하고 `mapping.go`에 "Config is data, not
code" 규율이 있다. **DB 불필요**. 엔진 **로직**만 Go에 남고, 룰 **데이터**는 config 템플릿에 산다
(재컴파일 없이 배포·갱신; Nuclei 템플릿 배포와 동형). 이로써 부적합2(Go 데이터-테이블 이탈)도 해소.

**분리**:
- **엔진(로직, Go, observation)**: 룰을 순회/평가하는 코드 — 엔트로피 게이트·allowlist·JS-리터럴
  가드·path/body 매처·호스트 컨텍스트 가드. Shannon 엔트로피 헬퍼.
- **룰(데이터, config)**: **카테고리별 파일 = `configs/signatures/` 디렉토리**(유지보수·독립진화·
  diff 명확; `configs/mappings|coverage/`의 디렉토리+파일 관례와 동형). 파일↔소비 시그니처:
  ```
  configs/signatures/
    secrets.yaml             # FR-1~3, gitleaks 증류(전량)      → sensitiveCommentSig (PS-009)
    url-sensitive.yaml       # FR-8, ZAP InformationDisclosureInUrl → sessionInURLSig    (PS-018)
    api-docs.yaml            # FR-9, Nuclei exposures/apis        → adminPageSig        (PS-013)
    internal-disclosure.yaml # FR-10, ZAP Private-IP + 비-prod    → sensitiveCommentSig (PS-009)
  ```
  각 파일 예:
  ```yaml
  # secrets.yaml — distilled from gitleaks (MIT). rules: name/regex/minEntropy/allow
  rules:
    - { name: aws-access-key, regex: 'AKIA[0-9A-Z]{16}', minEntropy: 3.0, allow: 'EXAMPLE' }
    - { name: jwt,            regex: 'eyJ[..]\.eyJ[..]\.', minEntropy: 0 }
  # url-sensitive.yaml — distilled from OWASP ZAP (Apache-2.0)
  params:  [user, username, pass, password, pwd, token, ticket, session, jsessionid, sessionid]
  valuePatterns: [ '<credit-card re>', '<email re>', '<ssn re>' ]
  # api-docs.yaml — distilled from nuclei-templates (MIT)
  rules: [ { path: '/v3/api-docs', bodyMarker: '"openapi"\s*:' }, … ]
  # internal-disclosure.yaml — distilled from OWASP ZAP Private-IP (Apache-2.0)
  rfc1918: '…'  ; ec2Host: '…'  ; nonProdHosts: '(stage|staging|dev|qa|uat)[-.]|\.(local|internal|corp)\b'
  ```
- **로더(Go)**: **디렉토리 스캔**(fsec 자동탐색 로더 선례) — `configs/signatures/*.yaml`를 각 타입으로
  파싱, **부팅 시 RE2 컴파일·검증, fail-closed**(coverage 부팅 게이트 동형 — 잘못된 정규식/누락 시
  기동 거부, 조용한 무탐지 금지). 로더는 observation 패키지 측(cross-unit import 회피, arch-check 안전).
  cmd가 부팅 시 로드해 observation 엔진에 주입(현 `Default()` 대체/보강). 파일 부재 시 fail-closed.
- **소유권 불변**: 시그니처의 CWE 소유(1 sig = 1 CWE)는 구조적(코드)이지 룰 데이터가 아니므로,
  데이터 외부화는 boot gate·26 파티션·ownership에 무영향.
- **배포**: config 파일은 이미지에 동봉(mappings/coverage와 동일 위치 규율). Dockerfile `COPY configs/`.

**attribution**: config 각 섹션 헤더 주석 + 코드 로더에 출처·라이선스(gitleaks MIT / ZAP Apache /
Nuclei MIT) + "distilled at authoring, NOT executed at runtime" 명기.

**트레이드오프(정직)**: 하드코딩 대비 (a) config 파일 동봉·부팅 검증 machinery 추가, (b) 부팅 시
정규식 컴파일 비용(무시 가능). 이득: data-as-config 정합·재컴파일 없는 갱신·룰 추가가 config 편집.

## D4. 시그니처 취득 파이프라인 (Arch A · Level 2, 코드리뷰 G1 결과)

WebFetch는 요약이라 룰셋 전량 취득에 부적합. **재현 가능한 취득·변환 파이프라인**을 둔다(향후 모든
OSS 시그니처 확보의 표준).

- **취득(vendored, pinned)**: `third_party/<src>/`에 upstream 원본을 **PINNED 커밋 스냅샷**으로 동봉
  + `LICENSE` + `SOURCE`(URL·commit SHA). gitleaks(gitleaks.toml)·zap(param 목록/private-IP 데이터)·
  nuclei(exposures/apis subset). curl@pinned-SHA로 채움(offline 재현·supply-chain 정합).
- **변환(generator, Go)**: `tools/sigimport/`(소스별 어댑터 gitleaks.go/zap.go/nuclei.go) — upstream
  포맷(TOML/txt/YAML) 파싱 → 우리 통일 `configs/signatures/*.yaml`. **RE2 비호환은 등가변환 또는
  skip + 출력에 `# skipped:` 기록**(조용한 누락 금지). 각 출력 헤더에 provenance(source/commit/license/
  rules N/skipped M). 생성기는 **별도 tool 스코프**(런타임 바이너리 미포함; TOML 파서 등 dep 격리).
- **재현·갱신**: `make signatures` = `go run ./tools/sigimport` 재생성. **CI drift-check**:
  재생성 후 `git diff --exit-code configs/signatures/` — 커밋된 config가 pinned 소스+생성기와 일치 보장.
- **확장**: 향후 소스(secretlint/detect-secrets…)는 어댑터 1개 추가.

## 불변식 체크

26 파티션 · 소유권(CWE→PS-ID) · 체커/플랫폼 인터페이스 · 제품 Dockerfile · probe · platform seam.
변경은 observation 내부 + domain WorkItem 필드 추가(additive) + exec/fold 미러 + dedup 2차 패스.
