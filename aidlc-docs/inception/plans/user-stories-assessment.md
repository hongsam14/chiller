# User Stories Assessment — chiller (passive-scanner engine)

**Date**: 2026-07-08 · **Stage**: INCEPTION → User Stories (Part 1, Step 1 — mandatory validation)
**Inputs**: `../requirements/requirements.md` (approved), `../reverse-engineering/` (coolant, reference only)

---

## Request Analysis

| Field | Value |
|---|---|
| **Original Request** | Build `chiller`, a new independent, cloud-agnostic Go passive-scanner engine covering 25 of 26 KISA compliance items (PS-001…PS-026; PS-014 delegated to ASM) via three checkers — ZAP passive (14), 5 OSS probe adapters (5), 6 engine-native signatures — normalized into one Finding schema and one compliance report. |
| **User Impact** | **Direct** — chiller exposes an authenticated scan/status/report API (FR-09) consumed by external, uncontrolled systems, and produces a compliance report that humans act on. |
| **Complexity Level** | **Complex** — 7 heterogeneous tools orchestrated, unified normalization + dedup, task FSM with CAS-resume, partial-failure coverage accounting, production NFRs (Warm Standby DR, blue/green, OTel), plus a hard cloud-agnostic constraint. |
| **Stakeholders** | Compliance/audit owner (consumes the PS-ID report), security analyst (triages findings, judges false positives), platform/SRE operator (runs and scales the engine), upstream pipeline (crawler → chiller → downstream consumers), target-system owner (whose hosts get re-requested — protected by the FR-10 pre-approval gate). |

---

## Assessment Criteria Met

- [x] **High Priority — New User Features**: chiller is an entirely new product capability; every one of FR-01…FR-13 is new functionality.
- [x] **High Priority — Customer-Facing APIs**: FR-09 status/report API is consumed by *external, uncontrolled consumers* (established in Requirements Q3=A). Contract clarity is a stated, first-class constraint.
- [x] **High Priority — Multi-Persona System**: at least four distinct actors with divergent goals (compliance owner, security analyst, platform operator, upstream/downstream integrators) plus one affected non-user (target-system owner).
- [x] **High Priority — Complex Business Logic**: multi-scenario rules — PS-014 must render as *delegated* (not "gap", not "not found"); a failed tool must degrade only its own items (FR-08) while coverage accounting stays total over all 26 IDs (FR-13); dedup must prevent a single item being double-judged across checkers (FR-06). These are exactly the rules that get mis-implemented without explicit acceptance criteria.
- [x] **High Priority — Cross-Team Project**: spans security engineering, platform/SRE, and compliance; chiller runs as a *separate stage* alongside AI-Active-Scanner / Nuclei / ASM (C-2), so pipeline ownership is shared.
- [x] **Complexity — Risk**: chiller makes outbound requests against production targets. The pre-approval gate (FR-10) and throttling (NFR-13) are safety-critical; the abuse case is "scanner-as-DoS" (SECURITY-11). Misunderstanding here has real blast radius.
- [x] **Complexity — Testing**: PBT is enabled at FULL enforcement; the properties seeded in requirements §7 (coverage totality, dedup idempotence, severity totality) need story-level acceptance criteria to anchor them.
- [x] **Complexity — Ambiguity**: the false-positive posture ("anchors MUST be tight", FR-05) and the meaning of a *partially* successful scan are stated as intent but not yet as observable, testable outcomes.

**Not a skip case**: this is neither pure refactoring, an isolated bug fix, infrastructure-only work, developer tooling, nor documentation. None of the skip criteria apply.

---

## Decision

**Execute User Stories**: **Yes**

**Reasoning**: chiller meets six of the six High-Priority "ALWAYS Execute" indicators outright — it is a new user-facing capability, exposes a contract-bound API to uncontrolled external consumers, serves multiple personas with conflicting priorities, encodes multi-scenario business rules, and requires shared understanding across security, platform, and compliance teams. Requirements Analysis produced *system* obligations (FR/NFR); it did not produce *actor-centred, testable outcomes*. The gap between "FR-13: the report accounts for all 26 PS-IDs" and "as a compliance owner, I can tell a tool failure apart from a clean pass" is precisely what stories close, and it is the gap where a wrong implementation is most likely and most expensive.

---

## Expected Outcomes

- **Acceptance criteria that pin the tricky semantics**: `found` vs `not-applicable` vs `delegated` vs *tool-failed*, expressed as observable report states rather than prose intent.
- **PBT anchoring**: story-level criteria give the FULL-mode property tests (PBT-01) their oracles — coverage totality, dedup idempotence, severity mapping totality — before Functional Design has to invent them.
- **Safety made explicit**: the pre-approval gate and per-target throttling become acceptance-tested behaviours owned by a named persona, not footnotes.
- **API contract clarity**: with external uncontrolled consumers (Q3=A), stories fix what the report/status surface must guarantee, reducing the chance of a breaking change discovered in production.
- **Checker-seam validation**: writing "add a new checker without touching the orchestrator" as a story tests whether NFR-02's plugin seam — the seam coolant demonstrably lacks — is real or aspirational.
- **Scope defence**: stories make the non-goals (no crawling, no active attack, no coolant dependency) legible to every team touching the pipeline.

---

## Extension Compliance (this step)

| Extension | Status |
|---|---|
| security-baseline | Applicable rules (SECURITY-05/08/10/11/15) will be carried into stories as acceptance criteria on the pre-approval gate, API authz, input validation, and fail-closed behaviour. No blocking finding — this is an assessment artifact. |
| resiliency-baseline | Partial-failure / graceful-degradation obligations (NFR-12, FR-08, FR-13) are explicitly slated for story acceptance criteria. No blocking finding. |
| property-based-testing | PBT-01 property seeds from requirements §7 will be traced to story acceptance criteria. No blocking finding at this step. |
