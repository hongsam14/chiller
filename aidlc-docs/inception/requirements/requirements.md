# Requirements — chiller (passive-scanner engine)

**Project**: chiller · Service-Passive-Scanner · module `github.com/enki-offen/chiller`
**Date**: 2026-07-08 · **Depth**: Comprehensive (production-grade; security + resiliency + PBT extensions enabled)
**Driving plan**: `reference/augmentation_plan.html`
**Design references**: `../reverse-engineering/` (coolant — pattern source, not a dependency)

**Revision r2** (2026-07-08) — amended during User Stories planning per `../plans/story-planning-clarification-questions.md` (C4=A, C5=A/A, C6=A, C7=A/A, C8=A).
Change: **CWE becomes the canonical finding identity**; PS-ID and other compliance frameworks are *projections* of CWE through a new mapping layer (**FR-14**).
Affected: §2 (mission), §3 (FR-14 added), §6 (data model), §7 (new properties), §9 (new constraint), §11 (summary).
**Invariant preserved**: coverage accounting (FR-13) remains on the **PS-ID axis** — CWE is the *expression* axis, PS-ID is the *accounting* axis. Requires re-approval.

**Revision r5** (2026-07-08) — amended during CONSTRUCTION (domain unit) per `../plans/application-design-clarification-questions.md` Round 8 (HC0/HC1).
Change: **Post-MVP dynamic-JS discovery = Playwright network capture (asset-discovery mode)**, **superseding r4's static-manifest-enumeration** decision. Reason: static manifest enumeration is **bundler-coupled** (asset-manifest.json assumes webpack/CRA; breaks on Vite/esbuild/Next.js/obfuscation/non-exposed manifests → silently misses actionable first-party code-split chunks). Playwright network capture is bundler-agnostic. Also adds the **MVP known limitation** (dynamic JS uncovered in MVP) for audit defense (HC0). Affected: §2.2 (known limitation), §9 A-6. Requires re-approval.

**Revision r4** (2026-07-08) — amended during Application Design per `../plans/application-design-clarification-questions.md` (FC1/FC2, GC1-GC4).
Change: **PS-009 extended to "sensitive info in client-delivered source"** — HTML comments **+ statically-fetched JS** (comments / hardcoded secrets / internal endpoints), CWE-615/540/200. JS is reached by **shallow static discovery**: parse `<script src>` from the fetched HTML and GET those JS URLs **from approved hosts only** (GC2) — **no JS execution, no headless rendering** (rejected: heavier than the removed ZAP, and the execution-only JS is mostly non-actionable third-party/eval; the actionable first-party code-split chunks are reachable by *static* manifest enumeration). **Dynamic-JS inspection = Post-MVP via static bundler-manifest/chunk enumeration, not rendering** (GC4).
Affected: §2.1, FR-01 (shallow-discovery carve-out), FR-05, FR-10, §8, §9. Requires re-approval.

**Revision r3** (2026-07-08) — amended during Application Design per `../plans/application-design-clarification-questions.md` (Q2, AC1/AC6, BC1/BC2, DC1/DC2).
Change: **ZAP and Nuclei are removed from chiller entirely.** The checker layer becomes two families — **Observation** (in-process native engine, 10 items) + **Probe** (external CLI tools, 12 items) — plus **4 delegated** items. Items requiring error-triggering or active probing (PS-005 error page, PS-006 stack trace, PS-026 open redirect) are **delegated to the sibling Nuclei stage** (pipeline-level, like PS-014→ASM — chiller never calls Nuclei at runtime, so §2.2 independence holds). chiller now **covers 22 of 26 by observation, delegates 4**.
Affected: §1, §2 (mission/scope/composition), §3 (FR-03/04/05/10/11/13 rewritten), §6 (delegated target label), §8 (traceability), §9 (A-1 resolved, new C-6/A-5), §11 (summary). Requires re-approval.

---

## 1. Intent Analysis

| Field | Value |
|---|---|
| **User request** | Build the passive-scanner engine described in `augmentation_plan.html` using AI-DLC. First understand nuclei coupling in the reference system (coolant), then decide repo strategy, then build. |
| **Request type** | New Project (greenfield), informed by a brownfield reference (coolant). |
| **Scope estimate** | Cross-system in analysis (coolant reference), but Single-system in build: one new independent service. |
| **Complexity** | Complex — multi-tool orchestration (7 external CLI tools + in-process Observation engine), unified normalization, production NFRs, cloud-agnostic constraint. *(r3: ZAP/Nuclei removed; stack unified on the subprocess-CLI + in-process model.)* |

**Decision trail** (from `requirements-questions.md` + `-round2.md`): new **independent** Go repo copying coolant's engine-neutral patterns with **no shared code** and **no runtime dependency** on coolant (the 6 native checks stay in chiller, not delegated to Nuclei). coolant is a reference only.

---

## 2. Mission & Scope

**Mission.** Close the KISA compliance coverage gap for **26 items (PS-001…PS-026)** — hygiene/configuration checks (security headers, cookie attributes, version/info disclosure, TLS, etc.) that are judged by **observation and configuration inspection**, not by active attack. These are not covered by the existing AI-Active-Scanner (injection/payload attacks) or Nuclei (CVE PoC). *(r3: items that cannot be judged by pure observation — PS-005/006 error-triggering, PS-026 open-redirect — are delegated to the Nuclei stage, not attempted by chiller.)*

**Operating premise.** Target **URLs are already collected** by an upstream crawler (response bodies are NOT stored). chiller does not crawl or discover — it **re-requests the supplied URLs at scan time and inspects the responses**.

**Finding taxonomy (r2).** chiller emits **weaknesses**, not attack patterns — every finding's canonical identity is one or more **CWE** IDs. Compliance frameworks are *views* over that canonical set: KISA PS-ID is the framework implemented now; **OWASP Top 10** (whose official taxonomy is itself defined over CWEs) and **CAPEC** (derivable via MITRE's `Related_Weaknesses` relation) are additional projection targets, tagged Post-MVP. Adding a framework must require **only a new mapping table**, never a change to any checker (see FR-14, NFR-15).

### 2.1 In scope — chiller covers 22 of 26 items via two checker families *(rewritten in r3)*

| Family | Items | Count | Method |
|---|---|---|---|
| **Observation** — in-process native engine | PS-003, 008, 009, 010, 011, 012, 013, 018, 022, 023 | 10 | Benign GET (incl. well-known paths + *(r4)* `<script src>`-discovered first-party JS, approved hosts only) + tight anchors over HTML/JS/headers/cookies. No error-triggering, no JS execution, no attack payloads. Implemented in chiller. |
| **Probe** — external CLI adapters | PS-001, 015, 016, 017, 024 (humble) · 007, 020 (httpx) · 002 (testssl.sh) · 004 (dirhunt) · 019 (changeme) · 021 (Corsy) · 025 (subzy) | 12 | 7 version-pinned CLI tools behind a uniform adapter interface, one subprocess model. |

**ZAP removed (r3):** its 14 observation items are re-homed — headers/cookies → **humble** (Probe), versions → **httpx** (Probe), body/well-known-path signatures → the in-process **Observation** engine, and the error-triggering ones → delegated (below). No JVM-resident dependency remains.

### 2.2 Out of scope (non-goals) & delegations

**Delegated (4 items — accounted as `delegated`, never as a gap; each carries a target label):**
- **PS-014 (unnecessary HTTP methods)** → **ASM**.
- **PS-005 (error page info disclosure), PS-006 (stack trace)** → **Nuclei stage** — judging these requires *triggering* an error/exception, which is not pure observation (r3, DC2).
- **PS-026 (open redirect)** → **Nuclei stage** — requires active redirect probing, not passive observation (r3, BC1/DC1).

**Known limitation (MVP, r5 / HC0):** MVP JS discovery is `<script src>`-static only (C-8) and does **not** cover dynamically-loaded / code-split JS. That class is a **documented coverage gap in MVP**, closed Post-MVP by the A-6 Playwright asset-discovery mode. Auditors must read an MVP scan's dynamic-JS exposure as *"not inspected"*, not *"clean"* — the gap is explicit, not silent.

**Non-goals:**
- **Active attack / injection / CVE PoC** — belong to AI-Active-Scanner and Nuclei (separate stages, not run concurrently with chiller; C-2).
- **Asset discovery / crawling** — upstream; chiller consumes a URL list.
- **No coolant/Nuclei/ZAP dependency** — chiller contains no ZAP and no Nuclei. Delegation to the Nuclei stage (PS-005/006/026) is **pipeline-level** (chiller marks the item `delegated → Nuclei`; the sibling Nuclei stage covers it independently; results are merged by the *upstream pipeline*, not by chiller). chiller has **zero runtime or code dependency** on coolant/Nuclei (Q1 round-2 = D preserved).

---

## 3. Functional Requirements

| ID | Requirement | Source |
|---|---|---|
| **FR-01** | Accept a scan request consisting of a **pre-collected target URL list** plus scope/auth context and per-scan options (throttle, tool selection). No crawling/link-following. *(r4 carve-out)* The **only** discovery permitted is **shallow, single-level, `<script src>` extraction** from a fetched HTML response to enumerate first-party JS URLs (approved hosts only, FR-10); this is not recursive crawling and does not follow navigations. | plan §01, §06; GC1 |
| **FR-02** | **Single-fetch reuse (cross-family)** *(strengthened r3, EC1-4)*: each in-scope URL's HTML response is fetched **once** and stored in a **transient ResponseCache** (§9 C-7); all HTML-fetching URL-scoped checkers — the Observation engine, humble, httpx — read from the cache instead of re-fetching, so fetch reuse spans **tool families**, not just within one worker. Protocol-specific tools (testssl, subzy, dirhunt) bypass the cache (EC3). Reduces target load (NFR-13, P6). | plan §06; EC1-4 |
| **FR-03** | **Observation checker** *(rewritten r3)*: an **in-process native engine** that judges 10 items (PS-003, 008, 009, 010, 011, 012, 013, 018, 022, 023) by benign GET (including well-known paths) + tight response body/header anchors. No ZAP. No error-triggering, no attack payloads. Anchors MUST be tight to suppress false positives. | plan §02/§04; AC6/DC2 |
| **FR-04** | **Probe adapters** *(expanded r3)*: integrate **7 external CLI tools** behind a uniform adapter interface, all version-pinned — **humble** (PS-001,015,016,017,024 headers/cookies), **httpx** (PS-007,020 versions), **testssl.sh** (PS-002), **dirhunt** (PS-004), **changeme** (PS-019), **Corsy** (PS-021), **subzy** (PS-025). Each adapter standardizes target input and result parsing. No Nuclei. | plan §03; Q2/AC2 |
| **FR-05** | **Observation signatures** *(renumbered scope r3)*: the 10 Observation items are implemented as benign GET + tight anchors — e.g. PS-008 robots.txt sensitive paths, PS-011 `.env` (`DB_PASSWORD=`/`APP_KEY=`), PS-012 backup files (`.bak/.zip/.old`), PS-013 admin page public access, PS-022 crossdomain.xml `domain="*"`, PS-023 CAPTCHA absence, plus the ZAP-migrated PS-003 (plaintext login form over `http://`), **PS-009 (sensitive info in client-delivered source — HTML comments + statically-fetched JS comments/secrets/endpoints, CWE-615/540/200)**, PS-010 (`.git` exposure), PS-018 (session ID in URL). Anchors MUST be tight. *(r4: PS-009 scans HTML + shallow-discovered first-party JS bodies; JS is fetched, never executed.)* | plan §04; AC3/AC5/BC2; FC1/GC3 |
| **FR-06** | **Normalization hub**: normalize all checker outputs to a unified record (**PS-ID · severity · evidence**), deduplicate so no item is double-judged across checkers, and merge into a single compliance report. | plan §02, §06 |
| **FR-07** | **Task lifecycle**: create / start / stop / status / resume a scan task via an FSM with the compare-and-set / resume-leftover correctness pattern reused from coolant (`nuclei-coupling-analysis.md` §3–§5), generalized to a chiller work unit (not template IDs). | Q5=A; coolant ref |
| **FR-08** | **Fan-out execution**: a Scan(Task) fans out into **WorkItems** distributed with bounded concurrency; independent WorkItems run in parallel; one tool/WorkItem's failure MUST NOT abort the whole scan (partial results are reported). WorkItem grain is scope-based (§9 C-6). | plan §06; NFR-05; AC0 |
| **FR-09** | **Status & report API**: expose scan status, per-checker progress, and the merged report over an authenticated API (patterns copied from coolant's httpapi, cloud-agnostic). | coolant ref |
| **FR-10** | **Pre-approval gate**: every probe (humble, httpx, testssl.sh, dirhunt, changeme, Corsy, subzy), every Observation-engine GET, **and every `<script src>`-discovered JS fetch** (r4) executes **only against pre-approved / same-origin+allowlisted hosts** (GC2); unapproved hosts are rejected+logged before any outbound request. Third-party/CDN JS from unapproved hosts is not fetched. | plan footer; SEC-11; GC2 |
| **FR-11** | **Delegation with target label** *(generalized r3)*: the report explicitly marks delegated items as `delegated` with a **target** — PS-014 → `ASM`; PS-005, PS-006, PS-026 → `Nuclei`. `delegated` is distinct from "not found" and from "gap". Delegation is pipeline-level; chiller does not invoke the delegate. | plan §05; BC1/DC1/DC2 |
| **FR-12** | **Minimal evidence retention** *(clarified r3)*: chiller MUST NOT **persist** full response bodies **in the findings store**; only bounded evidence snippets needed to justify a finding are persisted. The **transient ResponseCache** (NFR-16) is *not* findings persistence — it is scan-scoped working state, encrypted and wiped post-scan — and is explicitly permitted. | plan §01; SEC-03; EC2 |
| **FR-13** | **Report completeness accounting**: for every scan, the report accounts for all 26 PS-IDs as covered / not-applicable / delegated (with target — 4 items: PS-014→ASM, PS-005/006/026→Nuclei) / tool-failed, so partial tool failures are visible rather than silently dropped. Accounting is performed on the **PS-ID axis**, not the CWE axis. | resiliency; plan §05 |
| **FR-14** | **Compliance framework mapping** *(new in r2)*: every finding carries one or more **CWE** IDs as its canonical identity. A **mapping layer** — a declarative, versioned table, not code in a checker — projects CWE onto compliance frameworks. **MVP: KISA PS-ID only.** **Post-MVP: OWASP Top 10, CAPEC.** Mapping is **N:M** in both directions (one finding may carry several CWEs; one CWE may satisfy several PS-IDs). Checkers MUST remain ignorant of every compliance framework. Where a check has no clean CWE (known case: **PS-025 Subdomain Takeover**), the mapping table MUST record an explicit `no-canonical-cwe` marker rather than an invented ID. | Q10, C4=A, C5=A/A, C8=A |

---

## 4. Non-Functional Requirements

### 4.1 Architecture & implementation
| ID | Requirement |
|---|---|
| **NFR-01** | **Language & style**: Go. **Dependency injection**, **domain-driven package layout**, and **constructor/init enforced via Go interfaces** (per user preference). Engine, checkers, adapters, storage, and queue are each behind interfaces. |
| **NFR-02** | **Checker plugin seam**: Observation / Probe / Native checkers implement a common `Checker` (or `Analyzer`) interface so tools can be added/removed without touching the orchestrator — the seam coolant lacks (`nuclei-coupling-analysis.md` §5). |
| **NFR-03** | **Engine-neutral Finding type**: a single normalized finding schema (CWE, severity, evidence, source-tool, target) is the internal and API contract — defined once, since 7 heterogeneous tools must normalize to it. |
| **NFR-15** | **Compliance-framework extensibility** *(new in r2)*: adding a compliance framework (OWASP Top 10, CAPEC, …) MUST be achievable by adding a mapping table alone — no checker, orchestrator, or Finding-schema change. This is the seam that keeps FR-14's Post-MVP targets cheap. |

### 4.2 Cloud-agnostic (major cross-cutting constraint — Q4/Q7 round-2)
| ID | Requirement |
|---|---|
| **NFR-04** | **No AWS lock-in.** No hard dependency on AWS-specific services. Concretely: persistence on a self-hostable store (e.g., MongoDB/PostgreSQL, not DocumentDB-only features); queue on self-hosted Redis/Valkey (not SQS); object/artifact storage on MinIO/local (not S3-only); container images on any OCI registry (not ECR-only); no IRSA/instance-profile assumptions. Runnable on any Kubernetes or standalone. |
| **NFR-05** | **Reduced infra complexity** vs coolant: drop nuclei-specific machinery (template PVC/S3 distribution, Interactsh, Kueue-for-nuclei, DAST two-phase). Keep only what chiller needs. |

### 4.3 Resiliency (resiliency-baseline enabled; answers from `-round2.md`)
| ID | Requirement | Answer |
|---|---|---|
| **NFR-06** | **DR strategy: Warm Standby**, target RTO/RPO in **minutes**; data live, services at reduced capacity, scaled on failover. | Q2r2=C |
| **NFR-07** | **Regional topology: single-region, multi-AZ** (multi-region deliberately deferred to avoid AWS coupling). Warm Standby is realized within one region across AZs. | Q7r2=X |
| **NFR-08** | **Deployment: blue/green** cutover; **rollback: canary / health-metric auto-rollback**. (Blue-green + health-gated cutover reconciliation → NFR Design.) | Q6r2=C, Q5r2=C |
| **NFR-09** | **CI/CD: propose** a pipeline (baseline: Make/Docker/OCI/Helm like coolant, **AWS-free**). | Q4r2=B |
| **NFR-10** | **Change management & incident response: propose** lightweight processes (change record + approval + rollback note; IR + Correction-of-Errors). | Q3r2=B, Q8r2=B |
| **NFR-11** | **Observability**: OTel metrics/logs/traces on both control and data paths; per-checker progress metric (pattern from coolant `engine/progress.go`); health checks (shallow + deep to store / queue / ResponseCache / external CLI tool availability). *(r3: no ZAP.)* |
| **NFR-12** | **Fault isolation**: explicit timeouts on every outbound HTTP and every external-tool invocation; graceful degradation (a failed tool degrades coverage of its items, not the whole scan — see FR-08/FR-13); bounded concurrency to protect chiller and targets. |

### 4.4 Performance & safety
| ID | Requirement |
|---|---|
| **NFR-13** | **Throttling/rate limiting** of outbound requests per target to avoid overwhelming scanned hosts (passive, but still re-requests). |
| **NFR-14** | **Scale**: sized to configurable target-URL inventories; concurrency and batch sizes parameterizable (analogous to coolant's `MAX_TEMPLATES_PER_CLUSTER` / `WORKER_CONCURRENCY`). |
| **NFR-16** | **Transient ResponseCache** *(new r3, EC1-4)*: a Valkey-backed cache of fetched HTTP responses realizing FR-02's cross-family reuse. Entries use the **HAR (or HAR-subset)** schema (request URL/scheme, response headers, cookies, bounded body). Hard constraints: **scan-scoped TTL**, **wiped at scan completion**, **encrypted at rest** (SECURITY-01), **bounded max-body-size**, access-controlled. It is **working state, not findings persistence** (FR-12). Not a durability tier — loss of the cache degrades to re-fetch, never to data loss. |

---

## 5. Security Requirements (security-baseline enabled — all blocking)

chiller is itself a network-facing security tool that makes outbound requests and stores findings; it should also **dogfood** the hygiene it checks. Applicability of SECURITY-01…15:

| Rule | Applicability to chiller | Requirement |
|---|---|---|
| SECURITY-01 (encryption at rest/in transit) | **Applies** | Findings store encrypted at rest; TLS for all store/queue connections. |
| SECURITY-03 (app logging) | **Applies** | Structured logging; **no secrets/PII/credential evidence in logs** (findings may contain sensitive strings — redact). |
| SECURITY-04 (HTTP security headers) | **Applies (own API)** | chiller's own API sets the required headers / dogfoods PS-001,015-017,024. JSON-only endpoints: minimal set + documented. |
| SECURITY-05 (input validation) | **Applies** | Validate scan-request inputs: URL format/host allowlist, list-size bounds, option ranges. |
| SECURITY-06 (least privilege) | **Applies (infra)** | Service identity scoped; no wildcard IAM/RBAC. → Infrastructure Design. |
| SECURITY-08 (app-level access control) | **Applies** | Authn/authz on the API: who may launch scans / read findings; deny-by-default; token validation server-side. |
| SECURITY-09 (hardening, no default creds) | **Applies** | No default creds in chiller. Note: the **changeme** probe tests default creds *on targets* — that is its function, strictly gated by FR-10 pre-approval. |
| SECURITY-10 (supply chain) | **Applies (critical)** | **Pin versions** of the 5 external tools (testssl.sh, dirhunt, changeme, Corsy, subzy) and ZAP, plus Go deps (lock file); SBOM; **no `latest` image tags**; trusted sources only. |
| SECURITY-11 (secure design) | **Applies** | Probes gated (FR-10); rate limiting (NFR-13); security-critical logic (auth, pre-approval) isolated in dedicated modules; abuse case documented (scanner-as-DoS). |
| SECURITY-12 (auth/credential mgmt) | **Applies** | API auth; secrets (tool configs, store creds) via a secrets manager, not source. |
| SECURITY-13 (integrity) | **Applies** | Verify checksums/signatures of pinned external tools; no unsafe deserialization of tool output. |
| SECURITY-14 (alerting/monitoring) | **Applies (infra)** | Alerts on auth failures / abnormal scan volume; append-only audit of who scanned what. → Infra/NFR Design. |
| SECURITY-15 (fail-safe) | **Applies** | Every external tool/HTTP call has explicit error handling; fail-closed (unapproved target → reject); global error handler; resource cleanup. |
| SECURITY-02 (network intermediary logging) | **N/A at app level** → Infra Design (LB/gateway access logs). |
| SECURITY-07 (network config) | **N/A at app level** → Infrastructure Design. |

---

## 6. Data & Findings Model (requirements-level) — *revised in r2*

**Two axes, deliberately separated:**

| Axis | Purpose | Holder |
|---|---|---|
| **CWE** — *expression axis* | What weakness was observed. Canonical identity of a finding. Framework-agnostic. | `Finding.cwe_ids` |
| **PS-ID** — *accounting axis* | Whether the KISA compliance obligation is discharged. Preserves the 26-item totality invariant (FR-13). | `Scan.coverage` |

- **Finding** (normalized, emitted by checkers): `cwe_ids` (≥1, or the explicit `no-canonical-cwe` marker per FR-14), `severity`, `evidence` (bounded snippet), `source_tool` *(r3:* `observation:<id>` | `humble` | `httpx` | `testssl` | `dirhunt` | `changeme` | `corsy` | `subzy`*)*, `target_url`.
  - Checkers do **not** populate PS-IDs. A finding is framework-agnostic at emission.
- **Compliance mapping table** (declarative, versioned; FR-14): `cwe_id → { framework, framework_item_id }*`. MVP populates `framework = KISA` only. N:M in both directions. Versioned so a report records which mapping revision produced it.
- **Scan/Task**: id, target-URL set, requested checkers, FSM state, per-checker progress, and **coverage accounting over all 26 PS-IDs** (FR-13) with per-item status ∈ `{found, not-applicable, delegated, tool-failed}`. *(r3)* `delegated` carries a **target** field ∈ `{ASM, Nuclei}` (PS-014→ASM; PS-005/006/026→Nuclei); `tool-failed` records that a checker died before it could judge the item (FR-08).
- **WorkItem** *(r3, AC0/C-6)*: the fan-out unit. `URL-scoped` = `(single URL × all URL-scoped checkers)` (shares one fetch via the ResponseCache, FR-02; checks run in-process-parallel, EC4); `host-scoped` = `(single host × one host-scoped checker)`. A completion ledger over WorkItems is the source of truth for CAS-resume (FR-07), persisted in the Task store.
- **ResponseCache** *(r3, NFR-16)*: transient, scan-scoped, Valkey-backed, **HAR-schema** entries (URL/scheme, headers, cookies, bounded body). Encrypted at rest, TTL'd, wiped post-scan. **Distinct from** the persisted findings store — holds no findings, only working response data for cross-family fetch reuse.
- **Report**: findings (CWE-identified) + coverage accounting (PS-ID-identified) + mapping-table revision. Both axes are present; neither is derived at read time.
- **Retention**: no full response bodies (FR-12); evidence snippets only; findings encrypted at rest (SECURITY-01); redaction of sensitive evidence in logs (SECURITY-03).

---

## 7. Testable Properties (PBT-01 seed — full PBT enabled)

Flagged now for Functional Design / Code Generation (PBT-01 requires per-unit property identification):

| Candidate | Category | Note |
|---|---|---|
| Tool-output → Finding normalization | Oracle / Round-trip | A known tool-output fixture normalizes to a known CWE/severity; parsing is total (no panic on malformed output). |
| Finding dedup | Idempotence | `dedup(dedup(x)) = dedup(x)`; merging checker outputs is commutative in result set. |
| Coverage accounting (FR-13) | Invariant | For any scan, `found ∪ not-applicable ∪ delegated ∪ tool-failed = all 26 PS-IDs`, and the four sets are **pairwise disjoint**. |
| Severity mapping | Invariant | Every emitted finding has a valid severity; mapping is a total function over tool severities. |
| **CWE → framework projection** *(r2, FR-14)* | Totality | For every CWE a checker can emit, the mapping table yields either ≥1 framework item or the explicit `no-canonical-cwe` marker — never silence. |
| **Projection does not create coverage** *(r2)* | Invariant | Adding a framework's mapping table leaves the PS-ID coverage accounting bit-identical. The expression axis cannot move the accounting axis. |
| **Mapping N:M soundness** *(r2)* | Round-trip | For any finding `f` and framework `F`, every item in `project(f, F)` maps back to at least one CWE in `f.cwe_ids`. No item is conjured. |

Detailed property tables are produced per unit at Functional Design.

---

## 8. Traceability — PS-ID → checker → requirement

| PS-ID | Item | Checker | Requirement |
|---|---|---|---|
*(r3: Checker column reflects the 2-family + delegation composition. "Observation" = in-process native engine; "Probe" = external CLI.)*

| PS-ID | Item | Checker | Requirement |
|---|---|---|---|
| PS-001 | HSTS 미설정 | Probe: humble | FR-04 |
| PS-002 | TLS 버전/Cipher/인증서 | Probe: testssl.sh | FR-04 |
| PS-003 | HTTP 평문 로그인 폼 | Observation | FR-03, FR-05 |
| PS-004 | 디렉터리 인덱싱 | Probe: dirhunt | FR-04 |
| PS-005 | 에러 페이지 정보 노출 | **Nuclei (delegated)** | FR-11 |
| PS-006 | 스택트레이스 노출 | **Nuclei (delegated)** | FR-11 |
| PS-007 | 서버 버전 노출 | Probe: httpx | FR-04 |
| PS-008 | robots.txt 민감경로 | Observation | FR-03, FR-05 |
| PS-009 | HTML 주석 민감정보 *(r4: + 정적 JS 주석/시크릿, CWE-615/540/200)* | Observation | FR-03, FR-05 |
| PS-010 | .git 디렉터리 노출 | Observation | FR-03, FR-05 |
| PS-011 | .env 파일 노출 | Observation | FR-03, FR-05 |
| PS-012 | 백업 파일 노출 | Observation | FR-03, FR-05 |
| PS-013 | 관리자 페이지 노출 | Observation | FR-03, FR-05 |
| PS-014 | 불필요한 HTTP Method | **ASM (delegated)** | FR-11 |
| PS-015 | 쿠키 Secure 미설정 | Probe: humble | FR-04 |
| PS-016 | 쿠키 HttpOnly 미설정 | Probe: humble | FR-04 |
| PS-017 | 쿠키 SameSite 미설정 | Probe: humble | FR-04 |
| PS-018 | URL 내 세션ID 노출 | Observation | FR-03, FR-05 |
| PS-019 | 디폴트 관리자 계정 | Probe: changeme | FR-04 |
| PS-020 | 프레임워크 버전 노출 | Probe: httpx | FR-04 |
| PS-021 | CORS 과도한 허용 | Probe: Corsy | FR-04 |
| PS-022 | crossdomain.xml 과도 허용 | Observation | FR-03, FR-05 |
| PS-023 | CAPTCHA 부재 | Observation | FR-03, FR-05 |
| PS-024 | Clickjacking 방어 미흡 | Probe: humble | FR-04 |
| PS-025 | Subdomain Takeover | Probe: subzy | FR-04 |
| PS-026 | Open Redirect | **Nuclei (delegated)** | FR-11 |

**Composition summary (r3):** Observation (native) 10 · Probe (CLI) 12 · Delegated 4 (ASM 1, Nuclei 3) = **26**. chiller covers **22**.

**Note (r2)**: this table is the **accounting axis** — it proves all 26 items have a home. It is *not* the CWE mapping. Under FR-14 the checkers emit CWE-identified findings, and the mapping table projects those onto the PS-IDs above. Every row therefore also traces to **FR-14** (and to **NFR-15** for extensibility). The concrete `CWE → PS-ID` rows are authored at **Functional Design** (constraint C-5); PS-025 is expected to carry the `no-canonical-cwe` marker (assumption A-3).

---

## 9. Constraints & Assumptions

- **C-1** Upstream crawler supplies target URLs; response bodies are not available from it (chiller re-fetches).
- **C-2** chiller runs as a **separate stage** from AI-Active-Scanner / Nuclei / ASM (never concurrent).
- **C-3** *(r3)* External CLI tools (**humble, httpx, testssl.sh, dirhunt, changeme, Corsy, subzy** — no ZAP, no Nuclei) are runnable in chiller's runtime (containerized), **version-pinned** (SECURITY-10).
- **C-4** Cloud-agnostic: no AWS-only service is a hard dependency (NFR-04).
- **C-5** *(r2)* The **CWE ↔ PS-ID mapping table** (FR-14) is a first-class, reviewed artifact, not an implementation detail. Its concrete contents are produced and pinned at **Functional Design**; an indicative draft is recorded in `../plans/story-planning-clarification-questions.md` §C8 and is **not** authoritative.
- **C-6** *(r3)* **WorkItem scope model** (AC0): checkers are classified `URL-scoped` (Observation engine, humble, httpx, Corsy, changeme) or `host-scoped` (testssl, dirhunt, subzy). A `URL-scoped` WorkItem bundles one URL through all URL-scoped checkers (one shared fetch via ResponseCache, FR-02); a `host-scoped` WorkItem is one host through one host-scoped checker. Precise per-tool scope assignment is pinned at Functional Design.
- **C-7** *(r3, EC1-4)* **ResponseCache** (NFR-16) is a first-class Application Design component. Only HTML-fetching URL-scoped checkers (Observation, humble, httpx) use it; protocol-specific tools (testssl, subzy, dirhunt) bypass it. Its transient/encrypted/TTL/wipe constraints are hard (SECURITY-01, FR-12 boundary). Entry format = HAR (or subset). JS bodies discovered per C-8 are cached as their own HAR entries (bounded size still applies).
- **C-8** *(r4, GC1-4)* **Shallow static JS discovery**: chiller may parse `<script src>` from a fetched HTML response and GET those first-party JS URLs (approved hosts only, FR-10), **without executing JS**. This is the sole discovery carve-out to FR-01. No headless rendering. PS-009 signatures then scan the fetched JS bodies (comments/secrets/endpoints).
- **A-1** *(resolved r3, Application Design Q1=A)* Binary topology = **single image, role-selectable** (`--role=manager|worker`): manager runs API + FSM orchestration, worker runs checkers; workers scale horizontally. One artifact (NFR-05, cloud-agnostic).
- **A-2** Warm-Standby (NFR-06) vs single-region (NFR-07), and canary-rollback (NFR-08) vs blue/green-deploy, are reconciled at **NFR Design** (non-blocking).
- **A-3** *(r2)* **PS-025 (Subdomain Takeover)** is assumed to have no clean canonical CWE. If Functional Design finds one, the `no-canonical-cwe` marker is dropped for that item; the FR-14 totality property holds either way.
- **A-4** *(r2)* Success metric (C7=A): **coverage-accounting completeness is the goal**; scan throughput is a **constraint** whose numeric bound is set at **NFR Requirements** (NFR-14), not here.
- **A-6** *(r5, supersedes r4)* **Dynamic-JS discovery is Post-MVP via Playwright network capture — an "asset-discovery" mode, not DOM rendering and not static manifest enumeration.** Mechanism: load the page once (**depth-0**, no navigation-following, no interaction), record the JS URLs the page **actually requests** (`page.on("request"/"response")`), GET those from **approved hosts only** (FR-10), and feed the bodies to the PS-009 signatures. The output is the **JS-URL network log, not a DOM dump** (the DOM is screen output, not an asset; JS-body secrets/endpoints don't survive in it). This is bundler-agnostic — it catches code-split chunks, lazy `import()`, and runtime-injected scripts regardless of webpack/Vite/esbuild/Next.js. It **deliberately steps outside the pure-passive/no-execution premise** (the page's JS runs), accepted as a **bounded, opt-in Post-MVP** mode because complete source-exposure coverage (e.g. FSI item 43) requires it (HC1=A). *r4's static bundler-manifest enumeration is rejected: bundler-coupled, breaks on Vite/Next/esbuild/obfuscation/non-exposed manifests.*
- **A-5** *(r3)* The **Nuclei stage** (sibling; coolant/Service-Nuclei-Worker) is assumed to cover the 3 delegated items (PS-005/006/026). If it does not, they surface as `delegated → Nuclei` in chiller's report (visible, not a silent gap), and closing them is a pipeline concern, not chiller's.

---

## 10. Extension Compliance Summary (Requirements stage)

| Extension | Status at this stage |
|---|---|
| **security-baseline** | Requirements captured as §5 (SECURITY-01,03,04,05,08,09,10,11,12,13,15 applicable → requirements written; 06,14 → Infra Design; 02,07 → N/A at app level). No blocking finding: this is a requirements artifact, not yet code. |
| **resiliency-baseline** | Mandatory Requirements-phase questions answered (RTO/RPO, change mgmt, CI/CD, rollback, deployment, regional topology, incident response) → §4.3. DR strategy = Warm Standby. Testing approach (RESILIENCY-14) deferred to NFR Design as allowed. No blocking finding. |
| **property-based-testing** | PBT-01 property identification seeded (§7); full per-unit property tables produced at Functional Design. **r2**: three properties added to guard the new CWE→framework projection — totality, projection-does-not-create-coverage, and N:M round-trip soundness. The second is the one that keeps FR-13's invariant safe under FR-14. No blocking finding at Requirements. |

---

## 11. Key Requirements Summary

- A **new, independent, cloud-agnostic Go service** (`chiller`) that scans **pre-collected target URLs** to judge **22 of 26 KISA compliance items** by observation, delegating **4** (PS-014→ASM; PS-005/006/026→Nuclei stage), with **zero runtime dependency on coolant/Nuclei/ZAP**.
- *(r3)* **Two pluggable checker families** behind a common `Checker` interface — **Observation** (in-process native engine, 10 items) + **Probe** (7 external CLI tools: humble, httpx, testssl, dirhunt, changeme, Corsy, subzy — 12 items) — whose heterogeneous outputs normalize to **one Finding schema keyed by CWE**. No ZAP, no Nuclei in chiller.
- *(r2)* **CWE is the canonical finding identity; compliance frameworks are projections.** KISA PS-ID ships in MVP; OWASP Top 10 and CAPEC are Post-MVP and cost only a mapping table (FR-14, NFR-15). **Coverage accounting stays on the PS-ID axis** so FR-13's 26-item totality invariant survives the change.
- Reuses coolant's **task-lifecycle / CAS-resume / fan-out / OTel** *patterns* (copied, AWS-stripped), not its code.
- **Success = coverage-accounting completeness** (every scan classifies all 26 PS-IDs); throughput is a constraint, bounded at NFR Requirements.
- Production posture: **security-baseline** (all blocking), **Warm-Standby DR**, **blue/green + canary rollback**, **single-region multi-AZ**, **no AWS lock-in**, **full PBT**.
