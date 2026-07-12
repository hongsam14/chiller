# AI-DLC State Tracking — chiller (Service-Passive-Scanner)

## Project Information
- **Project Name**: chiller (passive-scanner engine)
- **Repo**: Service-Passive-Scanner · **Module**: `github.com/enki-offen/chiller`
- **Project Type**: Greenfield (new build), informed by a brownfield reference (`coolant`)
- **Start Date**: 2026-07-08
- **Current Stage**: ✅ All 4 units complete + Build and Test green + Operations (placeholder) reached (2026-07-10). Post-MVP: scan Fetcher retryablehttp resilience DONE. Active follow-ups tracked in Backlog.
- **Driving Plan**: `aidlc-docs/inception/requirements/reference/augmentation_plan.html` — KISA 컴플라이언스 26개 항목 passive-scanner 엔진

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

- [x] Build and Test — **COMPLETE (green) r2 2026-07-11** (infra increment folded in; approval pending). Prior r1 2026-07-10. All 4 units build + 103 unit tests + 7 integration scenarios pass, race-clean, gofmt clean, `make arch-check` OK, module coverage 66.0%. Instructions + summary in `construction/build-and-test/` (build/unit-test/integration-test/performance-test/security-test/summary). Awaiting approval → Operations.

## Infrastructure Plan (forward-reference)
- **`construction/infrastructure-design/infra-plan.md`** (2026-07-10) — consolidated plan for all deferred Infra work: seam-swap backends (TaskStore→Mongo, WorkQueue→Redis/Valkey, ProgressReporter→OTel, Authorizer→OIDC/mTLS, ResponseCache→Valkey, FindingsStore→Mongo), financial HTTPS/MITM hardening + LoginKeeper integration, probe CLI binaries + real parsers, PlaywrightDiscoverer, topology/deploy/NFR reconciliation. Principle: add impl **beside** the mem impl behind the **same interface**, reuse contract tests, select via `cmd/chiller` flags. Suggested order + acceptance criteria + cross-refs inside. **Pick any item there for the next task.**

## Backlog
- **retryablehttp-go for scan Fetcher resilience** — ✅ **DONE 2026-07-10**. Adopted `github.com/projectdiscovery/retryablehttp-go v1.3.17` in `internal/scan/fetch`: gated client wrapped with bounded exponential backoff, default `WithRetries(2)`. **Rate-budget constraint honored**: `checkRetry` charges `lim.Acquire(host)` per retry → each attempt spends one token (test-proven, no SR-4 bypass). Redirect re-gate preserved on both HTTPClient + the HTTP/2 fallback (`HTTPClient2` pointed at the same gated client → no fail-open seam). 5xx not retried (valid observed response); scope rejection fails fast; per-request ctx deadline remains the SR-8 bound. Tests in `retry_test.go`. Module green (build/vet/test/-race, gofmt, arch-check). Summary appended to `construction/scan/code/business-logic-summary.md`.

### 🟡 OPERATIONS PHASE
- [x] Operations — PLACEHOLDER reached 2026-07-10 (MVP). **Infra increment: Operations reached 2026-07-11** — `operations/production-readiness.md` records 3 mandatory pre-prod actions (run Docker Tier-2 integration suite; Go≥1.26.5 for GO-2026-5856; pin image digests) + carried limitations. Suggested next: ledger lease (safe split recovery), infra-plan #5/#6, probe binaries. (approved; no deployment build in MVP workflow)

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
