# AI-DLC State Tracking — chiller (Service-Passive-Scanner)

## Project Information
- **Project Name**: chiller (passive-scanner engine)
- **Repo**: Service-Passive-Scanner · **Module**: `github.com/enki-offen/chiller`
- **Project Type**: Greenfield (new build), informed by a brownfield reference (`coolant`)
- **Start Date**: 2026-07-08
- **Current Stage**: ✅ MVP (4 units + Build&Test + Operations) **plus 10 post-MVP increments (r7→r20), all merged to main** (main tip `9e761d0`, PR #28 merged 2026-08-04). The detection engine is now substantively wired — see **Implementation status** below. Active follow-ups in Backlog.
- **Driving Plan**: `aidlc-docs/inception/requirements/reference/augmentation_plan.html` — KISA 컴플라이언스 26개 항목 passive-scanner 엔진

## Implementation status (2026-07-29) — where the project actually stands

**26-item partition:** 13 judged by the **observation** engine · 9 by **probe** CLIs · 4 **delegated** by design (PS-005/006/026→Nuclei, PS-014→ASM). Honest current coverage ≈ **judges all but `changeme`(PS-019)**, with `subzy`(PS-025) needing its DB at runtime.

**Detection — DONE:**
- ✅ **Probe CLIs (r7/r8/r9)** — 6/7 wired + Docker-verified with real binaries & positive findings: humble(PS-001/…), httpx(PS-007/020), testssl(PS-002), dirhunt(PS-004), corsy(PS-021), subzy(PS-025). `changeme`(PS-019) stays a stub → honest `tool-failed`.
- ✅ **Observation (13 items)** — well-known-path GET provider delivers each artifact (r10); signatures **OSS-distilled** from gitleaks/ZAP/nuclei (r16, `configs/signatures/`); **FR-6** runs only the owning signature on a well-known sweep (kills the FP amplifier).
- ✅ **Transport correctness (r17)** — `br`/`zstd`/`deflate` bodies decoded (output-bounded, bomb-safe); body truncation surfaced in the report. Closes the former top correctness gap.
- ✅ **Reporting** — customer compliance report view `/report` + FSEC framework + per-request framework selection (#24); finding drill-down.

**The three 2026-07-13 "reality check" gaps are RESOLVED:** ~~12 probe items tool-failed~~ (r7/r8) · ~~6 observation items false-clean~~ (r10) · ~~shallow dictionaries + `br`/`zstd`/512KB transport blind spots~~ (r16 + r17).

**Remaining detection gaps (see Backlog → Forward TODO):** `changeme`(PS-019) unwired · discovery/subdomain scope (largest gap vs human DAST). ~~subzy DB (#0e)~~ **DONE r18** — PS-025 judged when the operator provisions the DB (`make subzy-db` + mount).

## Mission (from the plan)
Build a dedicated **passive-scanner engine** covering **KISA compliance 26 items (PS-001…PS-026)** —
security headers, cookies, info/version disclosure, TLS, open redirect, etc. — judged by **observation**,
not active attack. Composed of: ZAP passive (14) + 5 single-purpose probes (testssl.sh, Corsy, dirhunt,
changeme, subzy) + **6 engine-native signature checks** (PS-008/011/012/013/022/023). PS-014 → existing ASM.
Separate stage from AI-Active-Scanner, Nuclei, ASM.

## Reference Inputs (not this project's code)
- `aidlc-docs/inception/reverse-engineering/` — reverse-engineering of **coolant** (the nuclei orchestrator),
  copied here as design input. `nuclei-coupling-analysis.md` §3 lists coolant's engine-neutral substrate
  (Task FSM/CAS, asynq `TaskManager`, K8s `JobRunner`, Mongo, OTel) that chiller COPIES as patterns (Q5=A),
  with AWS specifics stripped (cloud-agnostic requirement).

## Requirements — Answers
Round 1 (`requirements.md` §Decisions): Q1=A Go (DI + domain + interface-forced construction) · Q2=A coolant
in prod · Q3=A external consumers · Q4=A separate deployables · Q5=A new independent repo, copy patterns,
no shared code · Q6→reverted to native (Q1 round2=D) · Q7/Q8/Q9=A extensions enabled.
Round 2: Q1=D 6 checks native (chiller fully independent) · Q2=C Warm Standby · Q3=B propose change mgmt ·
Q4=B propose CI/CD + **remove AWS dependency** · Q5=C canary rollback · Q6=C blue/green · Q7=X no multi-region ·
Q8=B propose IR+COE.

## Extension Configuration
| Extension | Enabled | Mode | Decided At |
|---|---|---|---|
| security-baseline | Yes | all blocking | Requirements Analysis (Q7=A) |
| resiliency-baseline | Yes | all blocking | Requirements Analysis (Q8=A) |
| property-based-testing | Yes | FULL (not Partial) | Requirements Analysis (Q9=A) |

## Scope Decision (2026-07-14) — passive core + active-probe coverage + session prerequisite
`inception/requirements/scope-decision-active-vs-passive.md`. 전수 조사 결과 chiller 자체엔 익스플로잇/퍼징 없음; 능동성은 OSS probe 5개(testssl/dirhunt/changeme/Corsy/subzy)에서만 유입.
- **D1**: 능동 probe 5개 **유지**(KISA 26커버리지). 미션 "passive" 순수성 완화는 의도적 수용; 신뢰경계(approved∩allowlist·rate·timeout) 안에서만.
- **D2**: 세션 설계 = "능동 전제"가 아니라 **로그인 뒤 표면 커버리지 전제(능동·수동 공통)**. 능동 5개 중 세션 쓰는 건 dirhunt·Corsy 2개뿐(testssl/changeme/subzy는 세션 무관). 세션이 진짜 살리는 건 인증 수동검사 + JS캡처 API 트래픽(스코프 본체).
- 스코프 정합 항목 17개(observation 10+humble 5+httpx 2) + 캡처 인프라 전부 IN. 능동 유지하되 익스플로잇/퍼징은 스코프 라인.

## ✅ CLOSED Increment (2026-07-15) — Probe 파서 실측 (#0d, r8)
Docker 실검증: chiller 이미지로 Juice Shop + **kblife-clone.com(실 HTTPS)** 스캔 → humble이 HSTS(CWE-319)·**clickjacking(CWE-1021)** 실 finding 산출(재작성 전 0개), corsy CORS 실 finding, testssl HTTPS 실행(TLS 양호), 쿠키 소유권 이전 정상(Set-Cookie 없는 페이지 N/A), 26 파티션·정직성 유지. Build&Test(로컬 green)·Operations(production-readiness #0d partial-DONE) 마감. 다음: testssl/dirhunt/subzy positive 실측(취약 대상 확보 시).

## ✅ CLOSED Increment (2026-07-31) — Evidence 강화: 구조화 스키마 (r20)
**전 AI-DLC 단계 완료.** Requirements(명확화: evidence는 고객 UI가 아니라 다운스트림 AI 기계판독 근거 — 능동 공격 플래너+정오탐 체커; 매치위치·룰/가설·요청/응답 컨텍스트 4개 전부, 스키마 교체, DTO 확장) → Workflow Planning(EXECUTE 4/SKIP 6, Risk=Medium) → Functional Design(자기검증 F1~F4 교정) → Code Gen P1(자기검증 PD-A/PD-B 교정) → **Code Gen P2(kernel-panic 다중 중단 → 재개 검증: 코드+테스트 이미 완성·전 게이트 green, 플랜 A~I 누락 없음)** → **코드리뷰(workflow high, 16에이전트→6확정, data-loss 2건 포함 전부 수정+회귀)** → Build&Test(로컬 전 게이트 green) → Operations 도달. 커밋 `8f0f498`(feat)·`564285b`(docs)·`071ad94`(B&T) on feat/evidence-enrichment. **핵심**: `domain.Evidence{Snippets []string}`→`Evidence{Items []EvidenceItem{Snippet,Location,Rule,Hypothesis}, Request*, Response*}`. observation 완전 채움(findingAt가 소스교환 스레딩, matchContext offset/len, 13 시그니처 Location/Rule/Hypothesis, JS는 Location.SourceURL), probe best-effort(pFinding+hypothesisFor, Location/Req/Resp 날조금지 omitempty). seal/open은 전체 구조 직렬화(store EvidenceAAD + taskstore LedgerEvidenceAAD, Empty 게이트, **구 []string만 강등·그 외 손상은 에러**). **리뷰 최심각 F1/F2**(decrypt 후 unmarshal 실패 evidence 무언 소실→구 스키마 한정 강등). 리포트 findingDTO 구조화 + report_ui ev.snippet(bare-string 버그 수정). PBT FULL(경계·멱등·probe날조없음·seal왕복항등·강등vs손상·F1 JS회귀). 26-파티션·소유권·probe정직성·seam·**신규dep0** 불변. 남은 최상위 갭: 발견/서브도메인 스코프·changeme(PS-019).

## ✅ CLOSED Increment (2026-07-29) — subzy fingerprints DB provisioning (#0e, r18)
**전 AI-DLC 단계 완료.** Requirements(Q1~3 → 사용자 정정으로 operator-provision, Rationale 절 추가) → Workflow Planning → Infrastructure Design(코드젠 스모크가 Q2-A --vuln_list 없음 발견 → Q2-B HOME 전환) → Code Gen(P1+P2, 플랜 리뷰 P1~5 반영) → 코드리뷰(8건, 6수정/1검증/1스킵) → Build&Test(전 게이트 green + in-image A/B/C) → Operations 도달. 커밋 예정 on feat/subzy-db. **PS-025 살아남**: subzy v1.1.0는 fingerprint 플래그 없음 → `$HOME/subzy/fingerprints.json` 읽음. `make subzy-db`(pinned 5bd4e12 EdOverflow)가 git-ignored deploy/subzy/에 provision, operator가 /opt/subzy에 ro 마운트, 어댑터가 HOME=/opt로 실행. **정직성 subzy와 분리**: precheck가 subzy 읽는 정확한 파일이 present·non-dir·non-empty JSON 배열 아니면 tool-failed(false-clean 원천차단). 리뷰 최심각 F1(guard가 subzy 읽는 경로와 다른 리터럴 검사→false-clean) 수정. 데이터 미커밋(operator-provisioning 철학). 남은 detection 갭: changeme(PS-019)·발견/서브도메인 스코프. probe 인터페이스·파서(r9)·26 파티션·하드닝 불변.

## ✅ CLOSED Increment (2026-07-29) — P0 Transport Correctness (r17)
**전 AI-DLC 단계 완료.** Requirements(br/zstd/deflate 디코딩 + 512KiB truncation 노출, Q1~4) → FD(리뷰-교정: 스트리밍 출력-bound·JS-URL 전파·Truncated 정의 확장) → Code Gen(P1+P2) → 코드리뷰(10건, 8수정/1 no-change/1 skip) → Build&Test(전 게이트 green) → Operations 도달. 커밋 `f332503`(코드)·`baac256`(docs)·`eef5e14`(B&T) on feat/transport-correctness. **r10/r16 이월 "P0 transport" 최상위 갭 해소**: fetch/decode.go가 Content-Encoding 디코딩(출력-bound bomb-safe) + Report.TruncatedURLs로 truncation 노출. 리뷰 최심각 2건 사전차단(F1 Mongo seal이 Truncated 누락→프로덕션 무-op, F6 per-scan transport clone→풀재사용 상실+race). 신규 deps brotli·klauspost. 남은 갭: changeme(PS-019)·subzy DB(#0e)·x/text 범프(별도)·F9(latent HAR 헤더). `Fetcher` 인터페이스·26 파티션·안전봉투·Dockerfile·seam 불변.

## ✅ CLOSED Increment (2026-07-29) — Observation 오탐 저감: OSS-증류 시그니처 + FR-6 소유-only (r16)
**전 AI-DLC 단계 완료.** Requirements → FD → Code Gen(P1+P2) → 코드리뷰(7건, 6수정/1 trade) → Build&Test(전 게이트 green) → Operations 도달(production-readiness r16 로그). 커밋 `0c4e0d3`·`f35305e`·`170d5c4`·`28364dd`·`1a4d5d8`·`e03b887` (feat/observation-fp). 남은 최상위 갭: **P0 transport br/zstd+512KB**(검출 엔진 최대 correctness 갭) · changeme(PS-019)·subzy DB(#0e).

Branch `feat/observation-fp` (off feat/compliance-report). Goal: Observation 오탐 대량 발생(kbland.kr 15건 중 13건 PS-009 jQuery FP, 4단 증폭) 근절 — 손수 정규식 대신 OSS(gitleaks/ZAP/nuclei) 시그니처를 **authoring-time에 증류**(distill, not use)해 RE2 포팅 + FR-6로 well-known 스윕이 소유 시그니처만 실행.
- **Requirements** — `inception/requirements/requirements-ps009.md` **approved**. FR-1..10(gitleaks secrets 전량+엔트로피·ZAP url params·nuclei api-docs·ZAP Private-IP RFC1918/EC2/비-prod=Gap B/FR-10·FR-6 소유-only). Nuclei/ZAP=증류 대상이지 런타임 사용 아님(명확화). 일부 갭(발견/서브도메인 스코프) 이월.
- **Functional Design** — `construction/scan/functional-design/observation-fp-functional-design.md` **approved**. D3(config 파일형=템플릿, DB 아님) 분리, FR-7 dedup-pass 이월(IdentityKey P2 위배). configs/signatures/ 카테고리별 yaml. 취득 파이프라인 D4(Arch A + Level 2 durable).
- **Code Generation Part 1 (Planning)** — `construction/plans/observation-fp-code-generation-plan.md` **approved** ("Continue"). A~F + 갭 G1~G7 반영. G1(curl@pinned-SHA raw + tools/sigimport 생성기).
- **Code Generation Part 2 (Generation)** — ✅ **CODE COMPLETE + 게이트 GREEN 2026-07-28, approval pending**. A(third_party/ pinned + tools/sigimport + make signatures/-check) · B(configs/signatures/{secrets 221·url-sensitive·api-docs·internal-disclosure}.yaml, provenance) · C(signatures_config.go fail-closed 로더 + Shannon, 3 시그니처 개조, Default→New(Rules)) · D(WellKnownPath{Path,CWE}, domain.WellKnownCWE, exec.observationForCWE 소유-only, finalize 미러, api-doc 선제) · E(--signatures 플래그) · F(distilled_sig_test/signatures_config_test/exec/orchestrator 코퍼스+ground-truth #3/#10/#15/#16+FR-6 미러+로더 fail-closed+RFC1918 PBT). 23 pkg green. 게이트: build/vet/gofmt/arch-check/race/**gosec 0**/**signatures-check drift 0**/**make secrets**(신규 `.gitleaks.toml`로 22 자기-매칭 해소). **D2 편차**: WellKnown bool + WellKnownCWE 병존(구 큐 하위호환). 26 파티션·소유권·인터페이스·probe·seam·제품 Dockerfile 불변. 커밋 `0c4e0d3`(FR-6+검증).
- **코드 리뷰(/code-review high) + 수정 2026-07-28** — 커밋 `f35305e`. 워크플로 17에이전트 → 7확정. **golang 철칙 위반 없음**(arch-check·RE2·fail-closed·정직성 미러·one-finding-per-CWE). 수정 6건(생성기 tools/sigimport에서 config 데이터 수정, 재생성 멱등): #1 api-docs 범용 path/marker drop(60→52/15→12, PS-013 FP), #2 PS-018 토큰-경계 매칭+bare user/pass drop(userType/passengers FP), #3 NonProdHosts 다중레이블 FQDN 요구(dev-mode/dev.js FP), #4 매트릭스 경로파라미터(`;jsessionid=`)+phpsessid/sid(FN), #6 scan() 소문자화 1회(perf), #7 dead ValuePatterns 제거. 회귀 테스트 전부 추가, 게이트 재검 green. **#5 스킵(의도적 trade)**: PS-009 범용 앵커(password:/api_key/todo) 제거 유지 — 저엔트로피 하드코딩·dev-note 미탐 tail 수용.
- **README 갱신 2026-07-28** — 커밋 `170d5c4`. Status/Roadmap 최신화(probe 6/7 배선·well-known 라이브·OSS-증류 시그니처·/report 뷰·framework 선택), 코드 대조 검증.
- **다음**: Code Gen Part 2 승인 게이트 → Build&Test → Operations. r16 도출 TODO는 Backlog 참조.

## ✅ CLOSED Increment (2026-07-24) — Observation 고도화: well-known-path GET provider + dictionary 심화 (r10)
**전 AI-DLC 단계 승인 완료.** Functional Design → Code Generation(P1+P2, +workflow-review 10건 전부 수정) → Build and Test(green) → Operations 전부 승인(2026-07-24). well-known provider가 6개 observation 항목의 false-clean/N-A 갭 해소; 5 시그니처 dictionary 심화(FP/FN 코퍼스). 고른 high 코드리뷰 10건(오탐 4·미탐 3·구조 3, 잠복 fold 정직성 버그 포함) 수정+회귀테스트. 로컬 게이트 green(build/vet+integration/gofmt/arch-check/race 23pkg-0FAIL/gosec 0). 26 파티션·소유권·인터페이스·Dockerfile 불변. 이월(정직): PS-009/018/023 dictionary(C1=A) · P0 transport br/zstd+512KB(C3=A). 다음 후보: PS-009/018/023 dictionary · P0 transport · §2 금융 세션 · §4 Playwright.

## (former active) Increment (2026-07-16) — Observation 고도화: well-known-path GET provider + dictionary 심화 (r10)
Branch (신규, off main). Goal: observation false-clean 해소 — approved 호스트에 well-known(robots/.env/.git/crossdomain/backup/admin) GET해 소유 시그니처에 아티팩트 전달 + 6개 항목 사전 심화(FN 확장 + FP 축소).
- **Requirements** — `inception/requirements/requirements-wellknown.md` **approved 2026-07-16**. Q1=A(fan-out well-known URL-scoped WorkItem 합성)·Q2=A(6개 항목)·Q3=A(approved 기본 on, gate+rate)·Q4=B+C1=A(배선+dictionary 심화, **배선 6개 항목만**; PS-009/018/023 다음 todo)·C2=A(FN확장+FP축소, 코퍼스)·Q5=A(r9 fixture dormant 활성화+통합 positive)·Q6=A(MaxURLs 제외)·C3=A(P0 transport 미포함). 명확화 1라운드 해소.
- **Workflow Planning** — `inception/plans/execution-plan-wellknown.md` **approved 2026-07-16** ("승인 & 계속"). Risk=Medium. EXECUTE: Functional Design · Code Generation · Build and Test · Operations. SKIP: Reverse/Stories/App Design/Units/NFR/Infra.
- **Workflow code review (high) 2026-07-24** — 10 findings CONFIRMED, **all fixed + regression-tested** (approval still pending). 4 new FP classes the sweep created (backup/admin soft-404 self-fire, env no path guard, robots db/api/sql substrings), 3 FN regressions vs r9 (env APP_KEY + value floor + placeholder map hid real weak creds, backupName whole-basename, loginMarker bare "login" suppressed real open-admin), 3 structural (probe amplification ×16 on synthesized items, sweep outside MaxURLs uncapped, host-major append broke PR-3a interleave). Fixes: signature path-guards/content-checks/segment-anchoring + `domain.WorkItem.WellKnown` (exec runs observation-only on well-known items; **latent fold honesty bug also fixed** — `foldOutcomes` reconstructed ran-checkers from scope alone and would have falsely marked skipped probes Checked) + `ScanOptions.DisableWellKnown`/`MaxWellKnownHosts`(50) bound + full URL-set interleave. Local gates green (build/vet+integration/gofmt/arch-check/race 23pkg/gosec 0). 26-partition/ownership/interfaces unchanged.
- **Code Generation (P1+P2)** — ✅ **APPROVED 2026-07-24** ("승인 & 계속"), incl. workflow-review 10-finding fixes above. → Build and Test. 요약 `construction/scan/code/wellknown-summary.md`. 신규 `observation/wellknown.go`(~16 경로) + fanOut 합성(PR-W1..6, authority 포트 보존) + 5 시그니처 v2 anchor(FN+FP) + FP/FN 코퍼스 27케이스 PASS(blocking) + fanout 합성 유닛 + r9 fixture well-known 표면 활성화(.env 현실secret·/db.sql·/admin·/.git/HEAD) + **end-to-end 실 orchestrator 스캔 PASS**(6 소유 CWE 전부 산출: 651/538/527/942/530/306, coverage 26). e2e 3건 수정(WorkerPool rate 상향·FinancialProxy atomic·slow-핸들러 경로 격리). 로컬 gates green(build/vet+integration/gofmt/arch-check/race/gosec 0). 26 파티션·소유권·인터페이스·제품 Dockerfile·probe·platform seam 불변. 발견: 합성 rate budget 공유로 호스트당 +~3s 지연(의도), Fetcher 자체 DNS(fixture는 IP 사용). 이월: PS-009/018/023 dictionary(C1=A), P0 transport(C3=A).
- **Functional Design** — `construction/scan/functional-design/wellknown-functional-design.md` **approved 2026-07-16** ("승인 & 계속"). WellKnown 경로 목록(~16 고정, host별 1회) + fanOut 합성 규칙(PR-W1..6: URLScoped 합성·MaxURLs 제외 구조상 보장·gate/rate 상속·coverage 불변) + 6개 시그니처 v2 anchor(FN 확장 + FP 축소): .env(키셋 일반화+플레이스홀더 배제)·backup(name heuristic, 단일 아카이브 미발화)·.git(`/\.git(/|$)`로 .gitignore/.github 배제 + HEAD)·admin(목록 확장 + SPA/login 마커 배제)·robots(키워드 확장)·crossdomain(유지). platform→scan import 확인(orchestrator가 이미 scan/{exec,fetch,ingest} import).

## ✅ CLOSED Increment (2026-07-15) — 검출 정합성 확대: testssl/dirhunt/subzy 실측 + 통합 취약 fixture (#0d 나머지, r9)
**전 AI-DLC 단계 승인 완료 + 실행 검증 완료.** Requirements→Workflow Planning→Infrastructure Design→Code Gen(P1+P2)→Build&Test→Operations 전부 승인. 실측으로 dirhunt·subzy 날조 스키마 파서버그 2개 교정(실 취약 출력에도 finding 0이던 것), testssl 스키마 정상 확인. in-image end-to-end positive 전부 PASS(dirhunt CWE-548·subzy takeover·testssl CWE-1327·humble/httpx/corsy). 신규 `test/fixtures/vuln-target/` 커밋(격리). **Operations**: production-readiness **#0d → DONE(6/6)**, 신규 **#0e**(subzy fingerprints DB 런타임 provisioning — 파서 검증 완료, DB 없으면 tool-failed) 기록. 6/6 probe positive 파서 실측 완료 → 검출 정합성 확대. 다음 후보: **#0e subzy DB 번들** · **🟠 observation well-known-path provider 배선(dormant 표면 재사용)** · §2 금융 세션 · §4 Playwright.

## (former active) Increment (2026-07-15) — 검출 정합성 확대 (#0d 나머지, r9)
Branch `feat/probe-parser-fidelity`. Target: testssl→외부 badssl.com, dirhunt/subzy→로컬 `test/fixtures/vuln-target/` fixture. Goal: 남은 3 probe positive-finding 파서 스키마 실측 + HTTP probe 5종(humble/httpx/Corsy/dirhunt/subzy) hermetic 통합 fixture 커밋(observation 표면 dormant).
- **Requirements** — `inception/requirements/requirements-detection-fidelity.md` **approved 2026-07-15**. Q1=A(badssl)·Q2=A(로컬 nginx autoindex)·Q3=A(mock 지문)·Q4=A(실 골든+이미지 positive)·Q5=A(3개 전부) + C1=B/C2=A(fixture 레포 커밋)/C3=A(스코프 라인)/C4=B(HTTP-only 통합, testssl 외부)/C5=B(observation 표면 dormant). 명확화 2라운드 해소.
- **Workflow Planning** — `inception/plans/execution-plan-detection-fidelity.md` **approved 2026-07-15** ("(A)"). Risk=Medium. EXECUTE: **Infrastructure Design(fixture 토폴로지, 경량)** · Code Generation · Build and Test · Operations(#0d 종결 갱신). SKIP: Reverse/Stories/App Design/Units/Functional Design/NFR(이미 requirements 열거). 제품 인터페이스·배포·Dockerfile 불변; 신규는 test/ 스캐폴딩.
- **Infrastructure Design (fixture 토폴로지)** — `construction/scan/infrastructure-design/vuln-fixture-design.md` **approved 2026-07-15** ("Continue to Next Stage"). IQ1=A(2 서비스: vulnweb nginx + takeover-mock)·IQ2=A(하이브리드 통합: httptest 유지 + dirhunt/subzy env-gated compose)·IQ3=A(호스트 미노출 내부 네트워크)·IQ4=A(subzy body-only 지문). security-baseline 격리 확정.
- **Code Generation Part 1 (Planning)** — `construction/plans/detection-fidelity-code-generation-plan.md` **approved 2026-07-15** ("Continue to Next Stage"). 11 steps.
- **Build and Test** — **green 2026-07-15, approval pending**. `construction/build-and-test/detection-fidelity-build-and-test-summary.md`. 로컬 gates 전부 green + in-image end-to-end positive 스위트 PASS(dirhunt/subzy/testssl/humble/httpx/corsy). fixture positive 재현 절차 + opt-in CI 잡 기록. 카운트/인터페이스 불변.
- **Code Generation Part 2 (Generation)** — **CODE COMPLETE + REAL-RUN VERIFIED 2026-07-15, approval pending**. 요약 `construction/scan/code/detection-fidelity-summary.md`. 실측으로 **2개 파서 실버그 교정**: dirhunt(실 신호 `index_of_processors[].crawler_url.flags:"index_of"` — 날조 `results[].flags` 폐기) + subzy(실 신호 문자열 `status:"vulnerable"` — 날조 boolean `vulnerable` 폐기); 둘 다 이전엔 실 취약 출력에도 finding 0. testssl/humble/httpx/corsy 스키마 정상 확인, 골든 hermetic 재캡처. 신규 `test/fixtures/vuln-target/`(2서비스 nginx+mock, 내부 vulnnet, 호스트 미노출, observation dormant 표면) + PBT-D1 + 하이브리드 통합 + opt-in CI 잡. **in-image end-to-end positive 전부 PASS**(실 어댑터 defaultExec→파서): dirhunt CWE-548·subzy takeover·testssl CWE-1327(rc4.badssl.com)·humble CWE-319/1021·httpx CWE-693·corsy CWE-346. 로컬 gates green(build/vet(+integration)/gofmt/arch-check/race/gosec 0). 정직성 불변·26 파티션·소유권·인터페이스·제품 Dockerfile 불변. **정직 이월**: subzy fingerprints DB 운영 갭(v1.1.0 self-fetch URL 죽음 → DB 없으면 tool-failed; 파서는 검증 완료, 런타임 DB provisioning은 후속) · observation dormant 표면은 well-known provider 배선 전까지 finding 미발생(별개 backlog).

## ✅ CLOSED Increment (2026-07-15) — Probe 파서 실측 (#0d, r8)
Branch `feat/probe-parser-fidelity` (off feat/probe-wiring). Target: OWASP Juice Shop 로컬 docker. Scope: humble 파서 실 섹션기반 재작성(현재 finding 0) + httpx 검증 + 기회적 Corsy; testssl/dirhunt/subzy 이월. Golden=실 출력.
Branch `feat/probe-parser-fidelity` (off feat/probe-wiring). Target: OWASP Juice Shop 로컬 docker. Scope: humble 파서 실 섹션기반 재작성(현재 finding 0) + httpx 검증 + 기회적 Corsy; testssl/dirhunt/subzy 이월. Golden=실 출력.
- **Requirements** — `inception/requirements/requirements-parser-fidelity.md` **approved 2026-07-14**. (C1=A Juice Shop 로컬·C2=B Juice Shop만·Q2=A humble+httpx·Q4=A 골든)
- **Workflow Planning · Code Gen(P1+P2)** — **완료 2026-07-15, approval pending**. humble 파서 섹션기반 재작성(헤더만)·Corsy 맵스키마 재작성·httpx 실 골든. **쿠키 소유권 이전(옵션 A)**: observation Set-Cookie 시그니처 3(CWE-614/1004/1275), humble=319/1021만. probe-owned 12→9, observation 10→13, 26 파티션 유지. 실 골든(Juice Shop/nginx) + **이미지 통합 positive 단언 green**(humble CWE-319·httpx CWE-693·corsy CWE-346). testssl/dirhunt/subzy positive 미실측 이월(production-readiness #0d partial). 로컬 build/vet/gofmt/arch-check/race/gosec(0) green. 요약 `construction/scan/code/parser-fidelity-summary.md`.

## ✅ CLOSED Increment (2026-07-14) — Probe CLI 배선 (r7) — Docker 검증·PR #11
PR #11 (feat/probe-wiring). 커밋 3fe8e63(배선)·d2ca0ac(Docker 교정)·2e18e79(UI coverage). **다음 증분 후보: #0d(취약 대상 파서 실측) · §2 금융 세션(JS 토큰→무거운 설계) · §3(FindingsStore/ResponseCache→durable, 일부 완료) · §5(OTel/OIDC) · §4(PlaywrightDiscoverer).**
Branch `feat/probe-wiring`. Scope: wire probe CLIs (infra-plan §3, Backlog TOP PRIORITY). **결과: 6/7 배선**(humble·httpx·Corsy·testssl·dirhunt·subzy), changeme(PS-019) 잠정 미배선(F5). 실행 시 최대 21/26 판정. **모든 AI-DLC 단계 승인 완료** + **실 Docker 검증 완료**(2026-07-14): 이미지 빌드 후 **6/6 probe가 이미지 내부에서 실 바이너리로 실행·파싱 clean 통과**(URL humble/httpx/corsy; Host testssl/dirhunt/subzy). 첫 실행이 F6를 현실화 — 파일출력 계약·툴 패키징(humble/Corsy=git, subzy=LukaSikic)·deps(hexdump/dig)·타임아웃·dirhunt 기존파일 버그·subzy HOME 전부 교정. production-readiness **#0a/#0b/#0c → DONE**. 커밋 3fe8e63(배선)+d2ca0ac(Docker 교정). **잔여 #0d(정직)**: positive-finding 스키마 미검증 — benign 대상은 empty/clean, humble 파서는 실 섹션기반 스키마와 달라 finding 미추출 → 취약 대상으로 파서/골든 실측 재작성 필요.
- **Requirements Analysis** — `inception/requirements/requirements-probe.md` **approved 2026-07-14** ("Approve & Continue"). Q1=A(전부 7개)·Q2=B(멀티스테이지 툴 이미지, non-distroless)·Q3=A(생태계 관용 pin)·Q4=C(유닛+통합 exec 검증, 좀비 방지)·Q5=A(JSON+골든)·Q6=A(tool-failed 불변).
- Implications: SECURITY-09(distroless) **amended** → NFR-P4 하드닝 리스크 수용(문서화). NFR-P1 서브프로세스 프로세스그룹 kill(좀비 방지).
- **Workflow Planning** — `inception/plans/execution-plan-probe.md` **approved 2026-07-14** ("Approve & Continue"). Risk=Medium. EXECUTE: Infrastructure Design · Code Generation · Build and Test (+ Operations 갱신). SKIP: Reverse Eng · User Stories · App Design · Units Gen · Functional Design · NFR Req/Design (NFR 이미 requirements-probe.md에 완전 열거). 단일 유닛 = scan(probe).
- **Infrastructure Design** — `construction/scan/infrastructure-design/{probe-infrastructure-design,deployment-architecture,compliance-summary}.md` **approved 2026-07-14** ("Continue to Next Stage"). IQ1=A(python:3.12-slim-bookworm)·IQ2=A(pipx per-tool venv)·IQ3=B(testssl 번들 openssl)·IQ4=A(빌드 이미지 내부 통합테스트)·IQ5=B(디스크 emptyDir 스크래치).
- **Code Generation Part 1 (Planning)** — `construction/plans/probe-code-generation-plan.md` **approved 2026-07-14** (+adversarial review F1-F6). 6 배선, changeme 잠정 tool-failed(F5).
- **Build and Test** — **local gates green 2026-07-14** (`construction/build-and-test/probe-build-and-test-summary.md`, approval pending). 로컬 실행·통과: build·vet(+integration)·gofmt·arch-check·`go test ./... -race`·**gosec 0**·**govulncheck 0**·probe 커버리지 94%. Docker 위임: 이미지 빌드·probe-integration(실 툴, 12→1)·trivy/syft·Mongo/Valkey 통합. F6: 파서 스키마/골든/Docker 핀은 CI 첫 빌드에서 확정.
- **Code Generation Part 2 (Generation)** — **CODE COMPLETE (local green) 2026-07-14, approval pending**. Delivered: `exec_linux.go`/`exec_other.go`(프로세스그룹 kill, NFR-P1, 손자사멸 유닛증명) · `parsers.go`(6 argv+JSON 파서) · `testdata/*.json` 골든 · `adapters.go` newStub→newWired(6) · 테스트 재작성(TestDefault_SixWired_ChangemeStub·TestWiredAdapter_MissingBinary_ReportsError·e2e 개명) · 통합 e2e `//go:build integration` · Dockerfile 멀티스테이지(python-slim+pipx+testssl번들openssl+go install httpx/subzy+non-root+/scratch) · worker.yaml scratch+TMPDIR · compose read_only+tmpfs+cap_drop · CI probe-integration 잡. **Verified local**: build/vet(+integration)/gofmt/arch-check OK, `go test ./... -race` green. **정직성 불변식 유지**: wired≠binary-present(부재 시 tool-failed). 카운트: 부재 12 / 통합 12→1(PS-019만). **F6 잠정**: 파서/골든/Docker 핀은 실 툴 출력 대비 미검증 → Build&Test(Docker)에서 확정. changeme(PS-019) 미배선. 멀티스테이지: go-build(chiller+httpx+subzy)+testssl(git tag+번들 openssl)→python-slim 런타임+pipx(humble/dirhunt/changeme/Corsy). 하드닝: non-root·read-only FS·cap drop ALL. blocking=0. 리스크: changeme Py2(Code Gen 해소)·이미지 크기.
- AI-DLC docs now tracked in repo (cde98c9); `.aidlc-rule-details/` restored from sibling repo.

## Stage Progress
### 🔵 INCEPTION PHASE
- [x] Workspace Detection (via relocation from coolant; greenfield build)
- [x] Reverse Engineering — reference only (coolant), reused
- [x] Requirements Analysis — requirements.md **approved** (2026-07-08, "Approve & Continue")
- [x] User Stories — **approved** 2026-07-08 (Part 2: personas.md 5 + stories.md 29/6 epics; control/data-plane principle added)
  - [x] Step 1: Assessment documented (`inception/plans/user-stories-assessment.md`)
  - [x] Step 2-6: `inception/plans/story-generation-plan.md` created with embedded questions
  - [x] Step 7-8: Answers collected (Q1=B, Q2=A, Q3=A+C, Q4=A, Q5=C, Q6=A, Q7=A, Q8=A, Q9=A, Q10=A+C+CWE)
  - [x] Step 9-10: Ambiguity analysis → clarification R2 (C1-C7) + R3 (C8). All answered A. **All ambiguities resolved.**
  - [x] Requirements r2 amendment (C6=A) — **approved** 2026-07-08 ("Approve & Continue")
  - [x] Step 13: Story plan approved — 2026-07-08 "Approve & Continue"
  - [x] Part 2: `personas.md` (5) + `stories.md` (29 stories, 6 epics) **approved** 2026-07-08
- [x] Workflow Planning — `execution-plan.md` **approved** 2026-07-08 (all stages EXECUTE)

### 🟢 CONSTRUCTION PHASE (planned — all EXECUTE, per-unit loop)
- [x] Application Design — **approved** 2026-07-08. 설계 질문 3라운드 전부 해소 (Q1-7, AC0-7, BC1-5, DC1-2). 재편 확정:
  - 체커 2계열: **Observation**(in-process native, 10: PS-003/008/009/010/011/012/013/018/022/023) + **Probe**(외부 CLI 7: humble/httpx/testssl/dirhunt/changeme/Corsy/subzy, 12)
  - **Delegated 4**: PS-014→ASM · PS-005/006/026→Nuclei stage (파이프라인 위임, chiller 런타임 미호출)
  - 토폴로지: 단일 이미지 role-flag(A-1 resolved) · WorkQueue 인터페이스+Redis/Valkey · Checker+ScanContext · 매핑=선언적 파일 · 단일 store+repository · ScanAPI(Platform) · WorkItem 2-scope(URL/host)
  - **ResponseCache** (r3, EC1-4): Valkey 휘발성 캐시, HAR 엔트리, cross-family fetch 재사용(FR-02 강화·NFR-16), 암호화·TTL·wipe (FR-12 경계)
  - 모든 설계 명확화 완료 (Q1-7·AC0-7·BC1-5·DC1-2·EC1-4). requirements r3 + stories **재승인 완료** (2026-07-08)
  - **산출물 5종 생성 완료 + 승인**: `inception/application-design/{components,component-methods,services,component-dependency,application-design}.md`
- [x] Units Generation — **approved** 2026-07-08 (4 units; order domain→findings→scan→platform)
  - Units: U0 domain (kernel) · U1 platform · U2 scan · U3 findings. Single deployable (manager/worker). Order: domain→findings→scan→platform.
  - **domain (U0): COMPLETE** — FD + Code Gen + code review fixes (M1 Finding.Validate, M2 fixed delegation map, L1-L4). Verified green. **Approved**.
  - **findings (U3): COMPLETE (approved)** — FD + Code Gen. 8 pkgs (config/severity/normalize/dedup/compliance/coverage/report/store) + P1-P7 property tests; build/vet/test/gofmt/arch-check green; yaml.v3 dep. Coverage = checker owns CWEs (FQ1) + attacker-axis `AttackTechnique` (FQ6: PS-025→ATT&CK T1584.001, subzy); `StatusChecked` (FQ2); severity config file (FQ4=B); `CoverageSpec` config-driven (B); P6 holds. NFR/Infra = N/A.
  - **scan (U2): CODE COMPLETE + code-review fixes applied** (approval pending) — 7 pkgs (ingest/safety/fetch/observation/probe/registry/exec) + tests; 10 observation signatures (tight anchors), probe exec framework (parsers stubbed→C-5), Discoverer seam (Static MVP; Playwright Post-MVP). **Cross-unit wiring test proves 22 covered + 4 delegated = 26.** **Workflow-backed review (high): 5H+6M+2L applied, each with regression test** — redirect gate re-check (H1), host-scoped gate (H2), rate default+burst clamp (H3/M1), per-request timeout+ctx cancellation (H4/M4), JS fan-out cap (H5), comment/git FP fixes (M2/M3), regex hoist (M5), pathOf net/url (M6), findings L1/L2 (domain.MaxEvidenceSnippets). **API change for platform: `Fetch(ctx,…)`/`Run(ctx,…)`/`exec.New(…,gate)`/`URLSet.Options`.** Prior: 질문1-5=A. Applied domain contract addition (`CheckResult` + `WorkItemOutcome.NotApplicable`, FQ2 — module re-verified green). Artifacts: `construction/scan/functional-design/{domain-entities,business-rules,business-logic-model}.md`. One-signature=one-CWE (질문1); subprocess exec (질문3); approval ∩ allowlist fail-closed (질문4); per-host token bucket (질문5).
  - **platform (U1): CODE GEN COMPLETE (approval pending)** (last unit — composition root). Part 2 generated + tested 2026-07-10. 8 platform pkgs (platform contracts / taskstore / workqueue / progress / api / orchestrator / proxy) + domain `TransportKind` + scan fetch `WithProxy` + probe transport + `cmd/chiller` + `test/platform_e2e_test.go`. All seams in-memory but Infra-swappable (ctx+error, CAS bool/error). Orchestrator: fan-out (host-interleave) + worker pool W + PR-5 fold + boot gate + resume/stop + net/http ScanAPI + Bearer authz. Financial auth-proxy (PR-12): BrowserSession(Playwright v0.5200.1 + FakeSession) + AuthProxy + 4 roles; **login keep-alive tab excluded from work tab pool T** (SessionConfig.ReserveKeepAlive + LoginKeeper seam; test asserts max concurrency == T). Deferred honestly: Redis/Mongo/OTel/OIDC backends + Chromium provisioning + MITM CA/HTTPS hardening + probe HTTP_PROXY env (binaries) + session auto-refresh. `go build/vet/test/-race` green, `gofmt -l` clean, `make arch-check` OK. Summary: `construction/platform/code/business-logic-summary.md`. Prior: FD COMPLETE, Q1-5=A (seam now, cloud backend→Infra). Artifacts: `construction/platform/functional-design/{domain-entities,business-rules,business-logic-model}.md`. Entities: Scan aggregate + ScanState FSM(created→running→{completed|stopped|failed}+resume) + WorkLedger(CAS-resume) + ProgressSnapshot + 5 seams(TaskStore/WorkQueue/Orchestrator/Authorizer/ProgressReporter, in-memory MVP). Rules PR-1..10: FSM table T1-6 all-CAS, fan-out by Scope, worker loop w/ ctx+isolation, PR-5 WorkItemOutcome→coverage.ScanOutcome fold, boot completeness gate(26-cover or refuse start), net/http 3 endpoints + Bearer authz fail-closed, --role=all default. Wiring: ingest→Executor.Run(ctx)→fold→Accountant.Account→report.Build.
  - HC1=A resolved → requirements **r5 approved** (Post-MVP dynamic-JS = Playwright asset-discovery net-capture; MVP known gap documented). All clarifications closed.

**Per-unit CONSTRUCTION stage matrix** (loop order domain→findings→scan→platform):
| Unit | Functional Design | NFR Req/Design | Infra | Code Gen |
|---|---|---|---|---|
| domain (U0) | ✅ approved | N/A | N/A | ✅ approved (green) |
| findings (U3) | ✅ approved | N/A | N/A | ✅ approved (green) |
| scan (U2) | ✅ approved | (numbers→Build/Infra) | deferred (Valkey/bundle) | ✅ complete (green), **approval pending** |
| platform (U1) | ✅ approved (+ browser-proxy amendment) | (numbers→Infra) | ✅ **APPROVED 2026-07-11** ("Continue to Next Stage") — scope = infra-plan §7 item 1 (WorkQueue→Valkey/Streams + TaskStore→Mongo). Q1-Q12+C1-C2 resolved (Q1=C·Q2=B·Q3=A·Q4=A·Q5=A·Q6=B·Q7=B·Q8=A·Q9=C·Q10=A·Q11=C·Q12=B·C1=A·C2=A). Artifacts: `construction/platform/infrastructure-design/{...}` + `shared-infrastructure.md`. blocking findings=0. **Infra Code Gen (increment) CODE COMPLETE (green) 2026-07-11, approval pending** — see Infra Increment row | ✅ **approved (green)** 2026-07-10 ("지금"→Continue) |

**Infra Increment (platform U1 seam-swap)** — ✅ **Code Gen APPROVED 2026-07-11** (incl. post-review hardening: workflow code review, 10 verified findings all fixed). Part 1 (plan `construction/plans/platform-infra-code-generation-plan.md`) + Part 2 (code) **CODE COMPLETE (green) 2026-07-11, approval pending**. 12 steps all [x]. Delivered: bson tags + string-enum marshaling (Q4=A, resolves F-3/F-4); contract-suite extraction (F-2) + PBT (math/rand oracle, PBT-02/05/06); `taskstore_mongo.go` (findOneAndUpdate CAS, majority WC); `workqueue_valkey.go` (single shared Stream + internal reader/reaper, C2=A seam-invariant, lazy drained-set Drain); `QueueInspector` optional iface + `/ui` dashboard + `/api/introspect/queue` + deep `/healthz` (SECURITY-04 now applies); `cmd/chiller` mem|mongo / mem|valkey switch (F-5, mem default); integration tests behind `//go:build integration` (testcontainers, Q10=A); Dockerfile + compose + k8s manifests + Makefile vuln/sbom/test-integration. Deps: mongo-driver v1.17.9, go-redis/v9 v9.20.1, testcontainers v0.37.0. Verified: build/vet/gofmt/arch-check OK, `go test ./...` pass, `-race` clean. Summary: `construction/platform/code/infra-business-logic-summary.md`. Interfaces UNCHANGED (PR-11). Deferred: integration run needs Docker; OTel/OIDC/ResponseCache/FindingsStore = infra-plan #3-#6.

- [x] Build and Test — **COMPLETE (green) r10 2026-07-24** (well-known increment + review fixes folded in; approval pending) — `construction/build-and-test/wellknown-build-and-test-summary.md`, all local gates green (build/vet+integration/gofmt/arch-check/race 23pkg-0FAIL/gosec 0). Prior r2 2026-07-11 (infra increment). Prior r1 2026-07-10. All 4 units build + 103 unit tests + 7 integration scenarios pass, race-clean, gofmt clean, `make arch-check` OK, module coverage 66.0%. Instructions + summary in `construction/build-and-test/` (build/unit-test/integration-test/performance-test/security-test/summary). Awaiting approval → Operations.

## Infrastructure Plan (forward-reference)
- **`construction/infrastructure-design/infra-plan.md`** (2026-07-10) — consolidated plan for all deferred Infra work: seam-swap backends (TaskStore→Mongo, WorkQueue→Redis/Valkey, ProgressReporter→OTel, Authorizer→OIDC/mTLS, ResponseCache→Valkey, FindingsStore→Mongo), financial HTTPS/MITM hardening + LoginKeeper integration, probe CLI binaries + real parsers, PlaywrightDiscoverer, topology/deploy/NFR reconciliation. Principle: add impl **beside** the mem impl behind the **same interface**, reuse contract tests, select via `cmd/chiller` flags. Suggested order + acceptance criteria + cross-refs inside. **Pick any item there for the next task.**

## Backlog

**Detection-fidelity priority order** — status as of r16 (2026-07-28). 세 페이로드/품질 갭 중 둘이 마감됨:
1. ✅ **Probe CLIs** — r7/r8에서 **6/7 배선**(humble·httpx·Corsy·testssl·dirhunt·subzy). `changeme`(PS-019)만 stub, ErrNotWired로 loud-fail. (아래 TOP PRIORITY 섹션은 이력으로 보존)
2. 🔴 **Signature transport correctness** — `br`/`zstd` 바디 + 512KB truncation은 *여전히 미해결*(글로벌 FN). r16 범위 밖으로 이월. (§ Signature quality → P0)
3. ✅ **Observation well-known-path GETs** — r10에서 배선(6 항목 아티팩트 전달). (아래 HIGH 섹션은 이력)
4. ✅ **Signature dictionary/value fidelity** — r16에서 OSS 증류로 대폭 해소(PS-009 토큰·PS-011 키·PS-012 backup-name·PS-018 sid·PS-013 admin/api-docs·Gap B 내부 disclosure). (§ Signature quality → P1은 이력; 잔존은 아래 r16 TODO)

### 📌 Forward TODO (2026-07-29, r17 종료 시점) — 우선순위 순
검출은 정직하고, 관측 엔진·transport·probe 대부분이 배선됨. 남은 것:

**검출 커버리지 (다음 increment 후보)**
- 🟠 **`changeme`(PS-019) — r19 조사 완료, stub 유지 결정.** 실 Docker 스모크(ztgrace/changeme v1.2.3)가 하드닝-미니멀 이미지에 **부적합** 판정: (B1) scan_engine이 telnet 스캐너 eager-import→`telnetlib`(PEP594, py3.13+ 제거)→python:3.14 base에서 import 불가, (B2) paramiko/selenium/DB드라이버 등 eager-import로 **~1GB + 대규모 공격표면**(SECURITY-09 충돌), (B3) 타겟당 ~24s. 도구 자체는 바운드 가능(--protocols http 기본·--fingerprint 오라클·creds baked)이나 **패키징 부적합**. 사용자 결정=**정직 바닥(stub 유지, PS-019 tool-failed)**. **미래 경로**: 상류가 telnet eager-import 제거+deps 경량화 시 재검토, 또는 **Option C(자체 경량 HTTP 기본자격증명 체크)** — 외부 바이너리·이미지 bloat 없이 PS-019 판정(권장 대안). 근거: `construction/plans/changeme-code-generation-plan.md` Step 0 RESULTS.
- ✅ ~~`subzy` fingerprints DB provisioning(#0e)~~ **DONE r18** — `make subzy-db`(pinned) + operator ro 볼륨 마운트(/opt/subzy) + HOME=/opt. 부재/빈/malformed → precheck tool-failed(false-clean 차단). PS-025 판정 가능.
- 🔴 **발견/서브도메인 스코프** — 사람 DAST 대비 **최대 갭**. 서브도메인 열거 + JS-발견 URL 확대(별개 대형 increment).

**완료됨 (이월 해소)**
- ✅ ~~`br`/`zstd`/`deflate` 바디 디코딩 + 512KiB truncation 노출~~ **r17 완료**.
- ✅ ~~observation dictionary 심화~~ **r16 완료**(OSS 증류).

**소규모 후속**
- ⚪ **severity.yaml 배선** — 파일·`findings/severity`·`findings/normalize`는 있으나 live 경로 미배선(체커가 직접 severity 설정).
- ⚪ **StopScan/ResumeScan HTTP 라우트** — 구현됨, 라우트 미노출.
- ⚪ **x/text→v0.39.0+ 범프** — GO-2026-5970(간접 dep, non-blocking).
- ⚪ **r17 F9(latent)** — 디코드 후 HAR의 압축 Content-Encoding/Length 헤더 잔존(소비자 없음). 디코드 시 strip.
- ⚪ **PS-009 저엔트로피/dev-note 미탐 tail**(r16 #5 trade) · **잔존 오탐 수용**(browser_password_hint 등) — 실측 보고 시 재검토.
- ⚪ **FR-7 dedup-pass 이월**(r16) — IdentityKey P2 멱등성 위배로 제외. 별도 설계 시 재기.

**대형 후속 (infra-plan §2/§4/§5)**
- ⬜ **§2 Financial 인증-프록시 하드닝** — Chromium provisioning, HTTPS CONNECT+MITM CA, storageState 세션 자동갱신, LoginKeeper 통합. (금융앱 JS-토큰 인증은 더 무거운 설계 — 상세는 아래 Deferred infra work.)
- ⬜ **§4 PlaywrightDiscoverer** — 동적 주입 JS 발견(정적 `<script src>` 파서가 놓치는 것). `hasPasswordField` SPA 오탐의 실제 해법.
- ⬜ **§5 OTel(ProgressReporter)·OIDC/mTLS(Authorizer)** — seam 존재, 옆에 구현체 추가.

### 📌 AI-DLC 진행 상태 (2026-07-31)
- ✅ **r7~r17 전부 main 병합 완료**(main `71ceabe`).
- ✅ **r20(evidence 강화) 전 단계 완료** on feat/evidence-enrichment — Requirements→Workflow Planning→FD(자기검증)→Code Gen(P1 자기검증 + P2 kernel-panic 재개검증)→코드리뷰(high 6수정)→Build&Test(로컬 green)→Operations 도달. 커밋 `8f0f498`/`564285b`/`071ad94`/`d515c4d` + CI 교정 `7a7a72a`(gitleaks allowlist: evidence seal 테스트 가짜 픽스처 `AbCdEf0123456789` 오탐 — full-history 스캔은 커밋 후에만 검출). **PR #28 merged 2026-08-04** (main tip `9e761d0`, 전 체크 green). 구조화 Evidence로 다운스트림 AI 기계판독 근거 제공. data-loss 리뷰 2건 수정(구 스키마만 강등).
- ✅ **r18(subzy DB #0e) 전 단계 완료** on feat/subzy-db — Operations 도달, PR/병합 대기(PR #26).
- ✅ **r19(changeme/PS-019) 조사 종결** on feat/changeme-wiring — Requirements(제한적 배선 + N/A applicability + well-known B 커버리지) → Workflow Planning → Functional Design(applicable 훅·well-known login 마커) → Code Gen P1(플랜+근거리뷰 F1~F7) → **Code Gen P2 Step 0 조기 스모크에서 STOP&REPORT**(telnetlib py3.13+ 부적합 + ~1GB). 사용자 결정=**stub 유지(정직 바닥)**. **코드 변경 0**(주석만: adapters.go/Dockerfile의 부정확한 "Python2 heritage" → 스모크-검증된 실제 이유로 정정). 다음: 발견/서브도메인 스코프 or Option C(자체 경량 PS-019 체크).

### 🔴 TOP PRIORITY — Wire the 7 probe CLIs for real (infra-plan §3)
**chiller judges only 10 of the 26 KISA items today.** No OSS tool has ever been executed as a
subprocess: all 7 probe adapters are unwired (`argv == nil`), every parser is `stubParse`, and the
Dockerfile installs none of the binaries — so `defaultExec` (the real `exec.CommandContext`) is
never reached and has zero test coverage.

- **Honesty fix landed 2026-07-13**: an unwired adapter now fails with `probe.ErrNotWired` →
  its items are reported **`StatusToolFailed` (no verdict)**, not `StatusChecked`. Previously they
  returned `(empty, nil)` and were accounted as **checked/clean**, so 12 of the 26 items were
  silently reported clean without any tool ever running — a false negative for every weakness they
  own. Guarded by `TestE2E_UnwiredProbes_ReportedAsToolFailed_NotChecked` (asserts 12 tool-failed /
  4 delegated / 10 judged) + `TestUnwiredAdapter_FailsWithErrNotWired`. `cmd/chiller` warns at boot.
  **Do not regress**: an adapter that cannot execute must fail, never return an empty clean result.
- **Remaining work** (per tool: humble, httpx, testssl.sh, dirhunt, changeme, Corsy, subzy):
  bundle the version-pinned binary in the image · author `argv` + a real `Parser` (stdout →
  `domain.Finding` with correct CWEs) · exercise `defaultExec` against a real binary at least once.
  Setting `argv` non-nil flips `Adapter.Wired()` and the item moves tool-failed → checked.
- **Acceptance**: the tool-failed count in the e2e coverage test drops by that probe's owned items
  and `checked` rises; coverage still partitions 26.

### 🔴/🟡 Signature detection quality — shallow dictionaries + transport blind spots (infra-plan §3c)
Adversarial review of all 10 observation regexes, 2026-07-13 (every match/no-match claim below was
verified empirically against Go RE2, 27/27). **Engine and FP-scoping design are strong; the
detection *dictionaries* and *value validation* are shallow, and two transport issues can nullify
even the working checks.** Split by nature:

**P0 — correctness (breaks even the currently-wired checks PS-003/009/018; do with the probe work):**
- **`br`/`zstd`/`deflate` bodies are never decoded.** The fetcher uses Go's default transport, which
  transparently handles **gzip only**. It sets no `Accept-Encoding`, so today servers reply gzip/
  identity and it works — but the moment a proxy/probe path advertises `br` (Cloudflare default) or
  `zstd`, every body regex runs against compressed bytes and matches **nothing**: a silent, invisible
  global false-negative. Decode by `Content-Encoding` (or force `identity`) before matching.
- **512 KB body cap truncates large payloads.** `io.LimitReader(body, 512KB)` on minified JS bundles
  (routinely >512KB) hides any secret/anchor past the cut; a `<!-- … -->` split at the boundary loses
  its `-->` so `htmlComment` skips the whole trailing comment. Consider a streaming/scan-then-drop
  read with a higher scan budget, or at least record when a body was truncated (don't report "clean"
  on a truncated body).
- **`hasPasswordField` gates THREE checks (PS-003/013/023) off one regex.** JS-rendered login forms
  (SPA) have no `<input type=password>` in the initial HTML → all three misfire (missed plaintext
  login + captcha, and admin flips to a false "unauthenticated" positive). Note as a known passive/
  no-JS limit; Playwright discovery (infra-plan §4) is the real lift.

**P1 — dictionary/value fidelity (do after payload delivery lands; each item verified):**
- **PS-011 `.env` — worst (FN+FP).** Only 4 keys (`DB_PASSWORD/APP_KEY/SECRET_KEY/AWS_SECRET_ACCESS_KEY`);
  `JWT_SECRET=`, `MAIL_PASSWORD=`, `STRIPE_SECRET_KEY=` (prefix breaks the `^` anchor) all **miss**
  (verified). No path guard → framework docs showing a `.env` example are flagged **Critical (FP)**.
  Fix: broaden key set + `KEY=value` shape/entropy, and only judge an actual `.env` artifact.
- **PS-009 sensitive-comment — weak secret detection.** Misses **JSON-style `"password": "…"`** (the
  intervening `"` defeats `password\s*[=:]`; only JS-object `password:"…"` matches — verified) and
  **all modern high-signal token formats** (`AKIA…`, `ghp_…`, `sk_live_…`, `AIza…`, `xoxb-…`, JWT
  `eyJ…` — all verified missed). ~7 patterns vs gitleaks/trufflehog's 100+. Fix: add a token-format
  set + a `"key"\s*:\s*"…"` JSON form.
- **PS-012 backup — FP at High + narrow FN.** Legit `report.zip`/`data.sql`/`archive.tar.gz` downloads
  flagged **High (FP)**; `.orig/.7z/.gz/.tgz/.backup/.save` **missed** (verified). Fix: pair extension
  with a backup-name heuristic (`index.php.bak`, `www.zip`, `db.sql`) instead of any archive.
- **PS-018 session-in-URL — `sid` too broad.** `/product?sid=12345` (store/screen id) flagged (FP,
  verified); `connect.sid` missed. Fix: drop bare `sid` or gate on token entropy/length.
- **PS-013 admin — narrow list + SPA FP.** Misses `/manager/html` (Tomcat), `/dashboard`,
  `/phpmyadmin`, `/administration`, `/backend` (verified); SPA login admin → false "unauthenticated".
- **PS-010 `.git`** — catches only `config`/`HEAD`; misses directory-listing exposures, `index`,
  `packed-refs`. Guard `Contains("/.git")` also matches `/.gitignore`, `/.github/` (loose).
- **PS-008 robots (Low)** — keyword list misses `/database`, `/sql`, `/.env`, `/logs`; Low severity
  limits impact. **PS-022 crossdomain** — regex solid; no `clientaccesspolicy.xml` (dead). **PS-023
  captcha** — fires on nearly every login (Low); JS-injected reCAPTCHA → false "absence".
- Also correct the `observation.Default()` comment calling these "safe stubs" — they are real anchors.

**Not a concern (stated for the record):** RE2 gives **linear-time matching — no ReDoS** is possible
on any of these patterns regardless of `.*`/`[^>]+` usage. Do not "harden" against catastrophic
backtracking; it cannot occur.

**Acceptance:** a curated FP/FN corpus (benign archive, framework `.env` doc, JSON secret blob, br-
encoded body, >512KB bundle, SPA login) — each asserts the correct verdict; anchors stay tight (no
new FPs on ordinary pages).

### 🟠 HIGH — Observation well-known-path GETs are not wired (FR-03/FR-05 gap)
**6 of the 10 observation items are effectively inert on a normal scan, and 4 of those report
false-clean.** The signature *logic* is real (unlike the probe stubs), but the engine only ever
judges the response of the **submitted URL**. `ingest.go` does no discovery beyond `<script src>`
JS, and nothing synthesizes the well-known artifacts these signatures need.

- **FR-03 requires "benign GET (including well-known paths)"** and the driving plan says explicitly
  "robots.txt를 다시 요청해 매칭", "crossdomain.xml을 요청해 검사". This is **not** crawling
  (FR-01 permits it) — the implementation simply omitted the well-known-path fetch step.
- **Observed on a bare-homepage scan** (proven by `TestE2E_UnwiredProbes…` counts: N/A=4, checked=6):
  - Genuinely assessed: PS-009 (comments+JS), PS-018 (session-in-URL) — payload *is* the submitted page.
  - **False-clean** (report "checked, no issue" without ever requesting the artifact):
    **PS-008 robots.txt · PS-011 .env · PS-012 backup files · PS-022 crossdomain.xml**.
  - Path-guarded, so N/A instead of false-clean (less harmful but still never actively fetched):
    PS-010 `.git`, PS-013 `/admin`.
- **Right fix (NOT interim N/A — these items ARE applicable to the host):** a host-scoped
  well-known-path provider that GETs `/robots.txt`, `/.env`, `/.git/config`, `/crossdomain.xml`,
  a small pinned backup-name list, `/admin` (approved hosts only, rate-budgeted) and feeds each
  response to the owning signature. New component (e.g. `WellKnownProvider`) + fan-out + tests.
- Also correct the misleading `observation.Default()` comment that calls these "safe stubs" — they
  are real anchors, just starved of input.
- **Acceptance**: on a target that actually exposes `/robots.txt`, `/.env`, etc., the corresponding
  PS item flips to a real finding; coverage still partitions 26; anchors stay tight (no new FPs).

### 🟣 Deferred infra work (still tracked — lived only in infra-plan; re-surfaced 2026-07-14)
These are NOT dropped; they are the planned follow-ups after probe wiring, and probe operation
**interacts** with them (see execution-plan-probe.md §4b G1-G4). User's order: probe → §2 → §4.
- **§2 Financial auth-proxy hardening (PR-12)** — Chromium provisioning + `playwright install`; HTTPS via
  CONNECT + bundled MITM CA (`authProxy.handle` currently rejects non-absolute-form); interception hardening
  (streaming/WebSocket/large bodies); **storageState lifecycle + session auto-refresh/token rotation**;
  **LoginKeeper integration** (팀원 login-agent가 `proxy.LoginKeeper` 구현 → 예약된 keep-alive 탭 구동; 인터페이스+슬롯 이미 존재). *probe 접점*: 프록시가 HTTPS 터널링을 못 하므로 §2 전까지 financial+HTTPS probe 불가(G2). **⚠️ probe가 작업 탭 풀 T를 소비함**(코드 확인: `authProxy.handle→session.Do→pool.acquire`) — 브라우저 종속 세션이라 인증 요청은 Chromium 컨텍스트로 재발행되고 T로 게이트됨. probe 트래픽이 네이티브 워커와 T를 경쟁하며, dirhunt 등 대량요청 probe가 T를 포화시킬 수 있음. **§2에서 T 사이징/예산 분리/직결 분기 결정 필요** — "work-pool 변경 불요" 전제는 probe 배선으로 무효화됨(G3). **⚠️ 세션 인증 방식(2026-07-14)**: 대상 금융앱은 JS 실행 종속 토큰(localStorage/in-memory Bearer) 가능성 큼. 현 `session.Do`=`bctx.Request().Fetch`(APIRequestContext)는 쿠키만 공유·JS 미실행이라 JS-부착 Bearer 안 실림. §2는 더 무거운 설계: (a) page.evaluate 토큰 추출+헤더 주입, (b) 인증 page 자체 `fetch` 재발행(요청당 실제 page=최중량). 둘 다 T가 실제-page 바인딩 → G3 심화. 상세: infra-plan §2 Design decision.
- **§4 Dynamic JS asset discovery (Post-MVP, r5/A-6/HC1)** — `PlaywrightDiscoverer`: Chromium으로 대상 로드,
  요청된 JS URL을 net-capture. 같은 `fetch.Discoverer` seam → drop-in. approved-host-only + fan-out cap 보존.
  *probe 접점*: URL-scoped probe 볼륨이 발견 URL 집합에 비례(G4); `hasPasswordField` SPA 오탐의 실제 해법.
- **Browser transport hardening** = production-readiness.md #4와 동일(§2 재진술).

### Done
- **retryablehttp-go for scan Fetcher resilience** — ✅ **DONE 2026-07-10**. Adopted `github.com/projectdiscovery/retryablehttp-go v1.3.17` in `internal/scan/fetch`: gated client wrapped with bounded exponential backoff, default `WithRetries(2)`. **Rate-budget constraint honored**: `checkRetry` charges `lim.Acquire(host)` per retry → each attempt spends one token (test-proven, no SR-4 bypass). Redirect re-gate preserved on both HTTPClient + the HTTP/2 fallback (`HTTPClient2` pointed at the same gated client → no fail-open seam). 5xx not retried (valid observed response); scope rejection fails fast; per-request ctx deadline remains the SR-8 bound. Tests in `retry_test.go`. Module green (build/vet/test/-race, gofmt, arch-check). Summary appended to `construction/scan/code/business-logic-summary.md`.

### 🟡 OPERATIONS PHASE
- [x] Operations — PLACEHOLDER reached 2026-07-10 (MVP). **Infra increment: Operations reached 2026-07-11** — `operations/production-readiness.md` records 3 mandatory pre-prod actions (run Docker Tier-2 integration suite; Go≥1.26.5 for GO-2026-5856; pin image digests) + carried limitations. **well-known increment: Operations reached 2026-07-24** — production-readiness r10 increment log: closes 6-item observation false-clean/N-A gap; new controls DisableWellKnown(off-switch, on-default) + MaxWellKnownHosts(50, skip+log); carried honest limits PS-009/018/023 dictionary(C1=A) + P0 transport br/zstd+512KB(C3=A, limitation 3b). Suggested next: PS-009/018/023 dictionary · P0 transport correctness · ledger lease · infra-plan #5/#6. (approved; no deployment build in MVP workflow) **r16 increment: Operations reached 2026-07-29** — production-readiness r16 increment log: OSS-distilled signatures (gitleaks/ZAP/nuclei) close the PS-009/018/013 dictionary gap + FR-6 owning-signature-only kills the well-known FP amplifier; carried honest limit narrows to **P0 transport br/zstd+512KB (now the top detection gap, limitation 3b)** + changeme/subzy DB. Code review 7건(6수정/1 trade). (approved; no deployment build in MVP workflow) **r17 increment: Operations reached 2026-07-29** — production-readiness r17 로그: P0 transport(br/zstd/deflate 디코딩+truncation 노출) 해소 → 한계 3b 전면 RESOLVED(r16+r17). 코드리뷰 10건 8수정. 다음 후보: changeme(PS-019)·subzy DB(#0e)·발견/서브도메인 스코프.

## Story Planning — Confirmed Decisions (Q1-Q10 + C1-C8)
Personas P1-P4+P6 (Q1=B) · hybrid domain-epic x journey x persona-tag (Q2=A) · 2-tier contexts
`Scan{Ingestion,Checking,Safety}` / `Findings{Normalization,Reporting}` / `Platform{Orchestration}` (C1=A) ·
Job Story for *situations* not actors (C2=A) · 6 native signatures promoted to individual stories (C3=A) ·
Given/When/Then AC, `Then` → PBT oracle (Q6=A) · partial-failure = first-class story, 4 states
`found/not-applicable/delegated/tool-failed` (Q7=A) · full coverage + MVP/Post-MVP tags (Q8=A) ·
`US-001…` + `Traces:` + reverse coverage table (Q9=A) · **CWE canonical, PS-ID/OWASP/CAPEC = projections**
(C4=A, C8=A) · N:M mapping, accounting on PS-ID axis (C5a=A) · KISA now, OWASP+CAPEC Post-MVP (C5b=A) ·
coverage completeness = goal, throughput = constraint deferred to NFR Requirements (C7=A/A).

## Open design questions — RESOLVED in platform Infrastructure Design (2026-07-11)
- ✅ Binary topology → single image + role-flag (Application Design A-1; reconfirmed).
- ✅ Warm Standby (Q2r2=C) vs no-multi-region (Q7r2=X) → **단일 리전 · 멀티 AZ** (Q11=C). Warm Standby = 리전 내 상시 멀티 AZ, 크로스리전 폐기. (deploy-arch §5)
- ✅ Canary (Q5r2=C) vs blue/green (Q6r2=C) → **rolling + 버전 고정 롤백** (Q11=C). 둘 다 폐기(배치 스캐너, 무중단 컷오버 불요). (deploy-arch §5)
- ✅ Throughput/duration (A-4) → **RPO≈0 · RTO 수 분** (Q12=B); throughput = worker Job 수 선형; p95 SLO는 fleet 확정 후 perf 곡선법 측정(Operations). (deploy-arch §5)
- (여전히) Concrete `CWE → PS-ID` mapping contents → 이미 Functional Design에서 처리됨(별개).

## Code Location Rules
- Application code: workspace root (NEVER in aidlc-docs/)
- Documentation: aidlc-docs/ only
